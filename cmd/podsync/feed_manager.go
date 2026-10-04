package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/update"
	"github.com/mxpv/podsync/services/web"
)

type AppFeedManager struct {
	mu          sync.RWMutex
	configPath  string
	cfg         *Config
	feeds       map[string]*feed.Config
	tokens      map[model.Provider]StringSlice
	downloader  *ytdl.YoutubeDl
	cron        *cron.Cron
	cronEntries map[string]cron.EntryID
	updates     chan *feed.Config
	updater     *update.Manager
	db          db.Storage
	storage     fs.Storage
	startedAt   time.Time
	version     string
	commit      string
	buildDate   string
}

func NewAppFeedManager(
	configPath string,
	cfg *Config,
	feeds map[string]*feed.Config,
	tokens map[model.Provider]StringSlice,
	downloader *ytdl.YoutubeDl,
	c *cron.Cron,
	cronEntries map[string]cron.EntryID,
	updates chan *feed.Config,
	updater *update.Manager,
	database db.Storage,
	storage fs.Storage,
	version, commit, buildDate string,
) *AppFeedManager {
	return &AppFeedManager{
		configPath:  configPath,
		cfg:         cfg,
		feeds:       feeds,
		tokens:      tokens,
		downloader:  downloader,
		cron:        c,
		cronEntries: cronEntries,
		updates:     updates,
		updater:     updater,
		db:          database,
		storage:     storage,
		startedAt:   time.Now(),
		version:     version,
		commit:      commit,
		buildDate:   buildDate,
	}
}

func (m *AppFeedManager) ListFeeds(ctx context.Context) ([]web.FeedSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []web.FeedSummary

	for _, f := range m.feeds {
		summary := m.buildSummary(ctx, f)
		result = append(result, summary)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

func (m *AppFeedManager) GetFeedDetail(ctx context.Context, id string) (*web.FeedSummary, []*model.Episode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	f, ok := m.feeds[id]
	if !ok {
		return nil, nil, errors.Errorf("feed %q not found", id)
	}

	summary := m.buildSummary(ctx, f)

	var episodes []*model.Episode
	_ = m.db.WalkEpisodes(ctx, id, func(episode *model.Episode) error {
		episodes = append(episodes, episode)
		return nil
	})

	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].PubDate.After(episodes[j].PubDate)
	})

	return &summary, episodes, nil
}

func (m *AppFeedManager) buildSummary(ctx context.Context, f *feed.Config) web.FeedSummary {
	summary := web.FeedSummary{
		ID:           f.ID,
		URL:          f.URL,
		Format:       string(f.Format),
		Quality:      string(f.Quality),
		PageSize:     f.PageSize,
		CronSchedule: f.CronSchedule,
		UpdatePeriod: f.UpdatePeriod.String(),
	}

	// Try to get enriched metadata from DB
	if dbFeed, err := m.db.GetFeed(ctx, f.ID); err == nil && dbFeed != nil {
		summary.Title = dbFeed.Title
		summary.Description = dbFeed.Description
		summary.Author = dbFeed.Author
		summary.CoverArt = dbFeed.CoverArt
		summary.Provider = string(dbFeed.Provider)
		if !dbFeed.UpdatedAt.IsZero() {
			summary.LastUpdate = &dbFeed.UpdatedAt
		}
	}

	if summary.Provider == "" {
		if info, err := builder.ParseURL(f.URL); err == nil {
			summary.Provider = string(info.Provider)
		}
	}

	// Count episodes, errors, total size
	var epCount, dlCount, errCount int
	var totalSize int64
	_ = m.db.WalkEpisodes(ctx, f.ID, func(ep *model.Episode) error {
		epCount++
		if ep.Status == model.EpisodeDownloaded {
			dlCount++
			totalSize += ep.Size
		} else if ep.Status == model.EpisodeError {
			errCount++
		}
		return nil
	})

	summary.EpisodeCount = epCount
	summary.DownloadedCount = dlCount
	summary.ErrorCount = errCount
	summary.TotalSize = totalSize

	// Generate URLs
	host := m.cfg.Server.Hostname
	if host == "" {
		port := m.cfg.Server.Port
		if port == 0 {
			port = 8080
		}
		host = fmt.Sprintf("http://localhost:%d", port)
	}
	host = strings.TrimSuffix(host, "/")

	mountPath := ""
	if m.cfg.Server.Path != "" {
		mountPath = "/" + strings.Trim(m.cfg.Server.Path, "/")
	}

	feedURL := fmt.Sprintf("%s%s/%s.xml", host, mountPath, f.ID)
	summary.FeedURL = feedURL

	cleanURL := strings.TrimPrefix(feedURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")

	summary.AppleURL = fmt.Sprintf("podcast://%s", cleanURL)
	summary.PocketCastsURL = fmt.Sprintf("pktc://subscribe/%s", cleanURL)
	summary.OvercastURL = fmt.Sprintf("overcast://x-callback-url/add?url=%s", url.QueryEscape(feedURL))

	return summary
}

func (m *AppFeedManager) AddFeed(ctx context.Context, newFeed *feed.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.feeds[newFeed.ID]; exists {
		return errors.Errorf("feed with ID %q already exists", newFeed.ID)
	}

	if newFeed.CronSchedule == "" {
		newFeed.CronSchedule = "@every 12h"
	}

	// Add to in-memory map
	m.feeds[newFeed.ID] = newFeed

	// Add to cron
	if m.cron != nil {
		feedToCron := newFeed
		cronID, err := m.cron.AddFunc(newFeed.CronSchedule, func() {
			log.Debugf("cron triggered for dynamic feed %q", feedToCron.ID)
			m.updates <- feedToCron
		})
		if err == nil {
			m.cronEntries[newFeed.ID] = cronID
		} else {
			log.WithError(err).Warnf("failed to register cron for feed %q", newFeed.ID)
		}
	}

	// Save to config.toml
	if err := m.saveFeedToConfig(newFeed); err != nil {
		log.WithError(err).Errorf("failed to save feed %q to config.toml", newFeed.ID)
	}

	// Enqueue for immediate update
	select {
	case m.updates <- newFeed:
		log.Infof("enqueued newly added feed %q for download", newFeed.ID)
	default:
		log.Warnf("update queue full, feed %q will update on next cron cycle", newFeed.ID)
	}

	return nil
}

func (m *AppFeedManager) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, exists := m.feeds[id]
	if !exists {
		return errors.Errorf("feed %q not found", id)
	}

	// Remove from cron
	if cronID, ok := m.cronEntries[id]; ok {
		m.cron.Remove(cronID)
		delete(m.cronEntries, id)
	}

	// Remove from map
	delete(m.feeds, id)

	// Remove from config.toml
	if err := m.deleteFeedFromConfig(id); err != nil {
		log.WithError(err).Errorf("failed to remove feed %q from config.toml", id)
	}

	// Clean database
	if err := m.db.DeleteFeed(ctx, id); err != nil {
		log.WithError(err).Warnf("failed to delete feed %q from database", id)
	}

	// Optionally delete files from disk/storage
	if deleteFiles {
		_ = m.storage.Delete(ctx, fmt.Sprintf("%s.xml", id))
		if m.cfg.Storage.Type == "local" {
			feedDir := filepath.Join(m.cfg.Storage.Local.DataDir, id)
			_ = os.RemoveAll(feedDir)
			log.Infof("deleted files for feed %q at %s", id, feedDir)
		}
	}

	_ = f
	return nil
}

func (m *AppFeedManager) TriggerUpdate(ctx context.Context, id string) error {
	m.mu.RLock()
	f, ok := m.feeds[id]
	m.mu.RUnlock()

	if !ok {
		return errors.Errorf("feed %q not found", id)
	}

	select {
	case m.updates <- f:
		log.Infof("manually triggered update for feed %q", id)
		return nil
	default:
		return errors.New("update queue is currently full, please try again shortly")
	}
}

func (m *AppFeedManager) RetryEpisode(ctx context.Context, feedID, episodeID string) error {
	return m.db.UpdateEpisode(feedID, episodeID, func(ep *model.Episode) error {
		ep.Status = model.EpisodeNew
		return nil
	})
}

func (m *AppFeedManager) GetTokens(ctx context.Context) []web.TokenInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := []struct {
		provider model.Provider
		envVars  []string
	}{
		{model.ProviderVkVideo, []string{"PODSYNC_VKVIDEO_API_KEY", "PODSYNC_VK_API_KEY"}},
		{model.ProviderYoutube, []string{"PODSYNC_YOUTUBE_API_KEY"}},
		{model.ProviderVimeo, []string{"PODSYNC_VIMEO_API_KEY"}},
		{model.ProviderSoundcloud, []string{"PODSYNC_SOUNDCLOUD_API_KEY"}},
		{model.ProviderTwitch, []string{"PODSYNC_TWITCH_API_KEY"}},
	}

	var result []web.TokenInfo
	for _, p := range providers {
		fromEnv := false
		for _, v := range p.envVars {
			if os.Getenv(v) != "" {
				fromEnv = true
				break
			}
		}

		var rawTokens []string
		if list, ok := m.tokens[p.provider]; ok {
			rawTokens = list
		}

		maskedTokens := make([]string, 0, len(rawTokens))
		for _, tok := range rawTokens {
			maskedTokens = append(maskedTokens, maskToken(tok))
		}

		result = append(result, web.TokenInfo{
			Provider: string(p.provider),
			Tokens:   maskedTokens,
			FromEnv:  fromEnv,
		})
	}

	return result
}

func maskToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "••••••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

func (m *AppFeedManager) UpdateTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	provider := model.Provider(providerStr)
	if m.tokens == nil {
		m.tokens = make(map[model.Provider]StringSlice)
	}

	var newTokens []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		newTokens = append(newTokens, tok)
	}

	if len(newTokens) > 0 {
		m.tokens[provider] = newTokens

		// Save to config.toml
		if m.configPath != "" {
			tree, err := toml.LoadFile(m.configPath)
		if err == nil {
			key := fmt.Sprintf("tokens.%s", providerStr)
			if len(newTokens) == 1 {
				tree.Set(key, newTokens[0])
			} else {
				tree.Set(key, newTokens)
			}
			f, err := os.Create(m.configPath)
			if err == nil {
				_, _ = tree.WriteTo(f)
				_ = f.Close()
			}
		}
		}
	}

	return nil
}

func (m *AppFeedManager) GetSystemStats(ctx context.Context) (*web.SystemStats, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	dataDir := m.cfg.Storage.Local.DataDir
	total, free, used, _ := getDiskSpace(dataDir)

	_, hasYtDlp := exec.LookPath("yt-dlp")
	_, hasFFmpeg := exec.LookPath("ffmpeg")

	stats := &web.SystemStats{
		Version:    m.version,
		Commit:     m.commit,
		BuildDate:  m.buildDate,
		Uptime:     time.Since(m.startedAt).Truncate(time.Second).String(),
		StartedAt:  m.startedAt,
		Goroutines: runtime.NumGoroutine(),
		MemoryMB:   float64(mem.Alloc) / 1024 / 1024,
		DiskTotal:  total,
		DiskFree:   free,
		DiskUsed:   used,
		HasYtDlp:   hasYtDlp == nil,
		HasFFmpeg:  hasFFmpeg == nil,
		DataDir:    dataDir,
		Storage:    m.cfg.Storage.Type,
	}

	return stats, nil
}

func (m *AppFeedManager) saveFeedToConfig(f *feed.Config) error {
	if m.configPath == "" {
		return nil
	}

	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}

	prefix := fmt.Sprintf("feeds.%s", f.ID)
	tree.Set(prefix+".url", f.URL)
	tree.Set(prefix+".format", string(f.Format))
	tree.Set(prefix+".quality", string(f.Quality))
	tree.Set(prefix+".page_size", int64(f.PageSize))
	if f.CronSchedule != "" {
		tree.Set(prefix+".cron_schedule", f.CronSchedule)
	}
	if f.Clean != nil && f.Clean.KeepLast > 0 {
		tree.Set(prefix+".clean.keep_last", int64(f.Clean.KeepLast))
	}
	if f.Filters.Title != "" {
		tree.Set(prefix+".filters.title", f.Filters.Title)
	}
	if f.Filters.NotTitle != "" {
		tree.Set(prefix+".filters.not_title", f.Filters.NotTitle)
	}

	file, err := os.Create(m.configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = tree.WriteTo(file)
	return err
}

func (m *AppFeedManager) deleteFeedFromConfig(id string) error {
	if m.configPath == "" {
		return nil
	}

	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}

	_ = tree.Delete(fmt.Sprintf("feeds.%s", id))

	file, err := os.Create(m.configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = tree.WriteTo(file)
	return err
}


func (m *AppFeedManager) GetDownloaderConfig(ctx context.Context) (*web.DownloaderConfigInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var dlCfg ytdl.Config
	var binPath, binVer string

	if m.downloader != nil {
		dlCfg = m.downloader.GetConfig()
		binPath = m.downloader.GetBinaryPath()
		if ver, err := m.downloader.GetVersion(ctx); err == nil {
			binVer = strings.TrimSpace(ver)
		}
	} else {
		dlCfg = m.cfg.Downloader
	}

	cookiesPath := dlCfg.CookiesFile
	if cookiesPath == "" && m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
		defaultPath := filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		if _, err := os.Stat(defaultPath); err == nil {
			cookiesPath = defaultPath
		}
	}

	hasCookies := false
	var cookiesSize int64
	var cookiesLines int
	var cookiesMod string

	if cookiesPath != "" {
		if fi, err := os.Stat(cookiesPath); err == nil {
			hasCookies = true
			cookiesSize = fi.Size()
			cookiesMod = fi.ModTime().Format("2006-01-02 15:04:05")
			if data, err := os.ReadFile(cookiesPath); err == nil {
				cookiesLines = len(strings.Split(string(data), "\n"))
			}
		}
	}

	return &web.DownloaderConfigInfo{
		Timeout:            dlCfg.Timeout,
		SelfUpdate:         dlCfg.SelfUpdate,
		CustomBinary:       dlCfg.CustomBinary,
		CookiesFile:        dlCfg.CookiesFile,
		CookiesFromBrowser: dlCfg.CookiesFromBrowser,
		Proxy:              dlCfg.Proxy,
		HasCookiesFile:     hasCookies,
		CookiesFileSize:    cookiesSize,
		CookiesFileLines:   cookiesLines,
		CookiesLastMod:     cookiesMod,
		BinaryPath:         binPath,
		BinaryVersion:      binVer,
	}, nil
}

func (m *AppFeedManager) UpdateDownloaderConfig(ctx context.Context, update *web.DownloaderConfigUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if update.Timeout != nil && *update.Timeout > 0 {
		m.cfg.Downloader.Timeout = *update.Timeout
	}
	if update.SelfUpdate != nil {
		m.cfg.Downloader.SelfUpdate = *update.SelfUpdate
	}
	if update.CustomBinary != nil {
		m.cfg.Downloader.CustomBinary = *update.CustomBinary
	}
	if update.CookiesFile != nil {
		m.cfg.Downloader.CookiesFile = *update.CookiesFile
	}
	if update.CookiesFromBrowser != nil {
		m.cfg.Downloader.CookiesFromBrowser = *update.CookiesFromBrowser
	}
	if update.Proxy != nil {
		m.cfg.Downloader.Proxy = *update.Proxy
	}

	if m.downloader != nil {
		m.downloader.UpdateConfig(m.cfg.Downloader)
	}

	return m.saveDownloaderToConfig(&m.cfg.Downloader)
}

func (m *AppFeedManager) GetCookiesContent(ctx context.Context) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cookiesPath := m.cfg.Downloader.CookiesFile
	if cookiesPath == "" && m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
		cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
	}

	if cookiesPath == "" {
		return "", nil
	}

	data, err := os.ReadFile(cookiesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(data), nil
}

func (m *AppFeedManager) UpdateCookiesContent(ctx context.Context, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cookiesPath := m.cfg.Downloader.CookiesFile
	if cookiesPath == "" {
		if m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
			cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		} else {
			cookiesPath = "cookies.txt"
		}
	}

	content = strings.TrimSpace(content)
	if content == "" {
		// Remove cookies file
		_ = os.Remove(cookiesPath)
		m.cfg.Downloader.CookiesFile = ""
		if m.downloader != nil {
			m.downloader.SetCookiesFile("")
		}
		return m.saveDownloaderToConfig(&m.cfg.Downloader)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(cookiesPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Wrap(err, "failed to create cookies directory")
		}
	}

	if err := os.WriteFile(cookiesPath, []byte(content+"\n"), 0600); err != nil {
		return errors.Wrap(err, "failed to save cookies file")
	}

	m.cfg.Downloader.CookiesFile = cookiesPath
	if m.downloader != nil {
		m.downloader.SetCookiesFile(cookiesPath)
	}

	return m.saveDownloaderToConfig(&m.cfg.Downloader)
}

func (m *AppFeedManager) TestDownloader(ctx context.Context, testURL string) (*web.TestDownloaderResult, error) {
	if m.downloader == nil {
		return nil, errors.New("downloader not initialized")
	}

	out, err := m.downloader.TestURL(ctx, testURL)
	if err != nil {
		cleanErr := out
		if cleanErr == "" {
			cleanErr = err.Error()
		}
		return &web.TestDownloaderResult{
			Success: false,
			Error:   cleanErr,
			Output:  out,
		}, nil
	}

	// Try parsing JSON metadata
	var meta struct {
		Title    string `json:"title"`
		Uploader string `json:"uploader"`
		Channel  string `json:"channel"`
	}
	_ = json.Unmarshal([]byte(out), &meta)

	channel := meta.Channel
	if channel == "" {
		channel = meta.Uploader
	}

	return &web.TestDownloaderResult{
		Success: true,
		Title:   meta.Title,
		Channel: channel,
		Output:  out,
	}, nil
}

func (m *AppFeedManager) saveDownloaderToConfig(cfg *ytdl.Config) error {
	if m.configPath == "" {
		return nil
	}

	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}

	if cfg.Timeout > 0 {
		tree.Set("downloader.timeout", int64(cfg.Timeout))
	}
	tree.Set("downloader.self_update", cfg.SelfUpdate)
	if cfg.CustomBinary != "" {
		tree.Set("downloader.custom_binary", cfg.CustomBinary)
	} else {
		tree.Delete("downloader.custom_binary")
	}
	if cfg.CookiesFile != "" {
		tree.Set("downloader.cookies_file", cfg.CookiesFile)
	} else {
		tree.Delete("downloader.cookies_file")
	}
	if cfg.CookiesFromBrowser != "" {
		tree.Set("downloader.cookies_from_browser", cfg.CookiesFromBrowser)
	} else {
		tree.Delete("downloader.cookies_from_browser")
	}
	if cfg.Proxy != "" {
		tree.Set("downloader.proxy", cfg.Proxy)
	} else {
		tree.Delete("downloader.proxy")
	}

	f, err := os.Create(m.configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = tree.WriteTo(f)
	return err
}
