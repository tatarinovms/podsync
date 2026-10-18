package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	"github.com/mxpv/podsync/pkg/logbuffer"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/update"
	"github.com/mxpv/podsync/services/web"
)

type feedErrorEntry struct {
	Error string
	Time  time.Time
}

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
	logBuffer   *logbuffer.Buffer
	feedErrors  map[string]feedErrorEntry
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
	logBuffer *logbuffer.Buffer,
) *AppFeedManager {
	mgr := &AppFeedManager{
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
		logBuffer:   logBuffer,
		feedErrors:  make(map[string]feedErrorEntry),
	}
	mgr.MountAllAudiobooks()
	return mgr
}

func (m *AppFeedManager) mountAudiobookFeed(f *feed.Config) {
	mounter, ok := m.storage.(fs.Mounter)
	if !ok {
		return
	}
	info, err := builder.ParseURL(f.URL)
	if err != nil || info.Provider != model.ProviderAudiobook {
		return
	}
	baseDir := ""
	if m.cfg != nil && m.cfg.Audiobooks.Dir != "" {
		baseDir = m.cfg.Audiobooks.Dir
	}
	resolvedDir, err := builder.ResolveBookDirectory(baseDir, f, info.ItemID)
	if err != nil {
		log.WithError(err).Warnf("failed to resolve directory for audiobook feed %q", f.ID)
		return
	}
	mounter.Mount(f.ID, resolvedDir)
	log.Infof("mounted audiobook feed %q -> %s", f.ID, resolvedDir)
}

func (m *AppFeedManager) MountAllAudiobooks() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, f := range m.feeds {
		m.mountAudiobookFeed(f)
	}
}

func (m *AppFeedManager) ListFeeds(ctx context.Context) ([]web.FeedSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]web.FeedSummary, 0, len(m.feeds))

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

// extractCoverArtFromXML reads the generated <feed_id>.xml from storage and extracts <image><url>...</url></image>
func (m *AppFeedManager) extractCoverArtFromXML(feedID string) string {
	xmlName := fmt.Sprintf("%s.xml", feedID)
	file, err := m.storage.Open(xmlName)
	if err != nil || file == nil {
		return ""
	}
	defer file.Close()

	buf := make([]byte, 32768)
	n, _ := file.Read(buf)
	if n <= 0 {
		return ""
	}

	xmlContent := string(buf[:n])
	// Match <image> ... <url>URL</url>
	re := regexp.MustCompile(`(?s)<image>.*?<url>\s*([^<\s]+)\s*</url>`)
	if match := re.FindStringSubmatch(xmlContent); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}

	// Fallback to <itunes:image href="URL"
	itunesRe := regexp.MustCompile(`<itunes:image\s+href="([^"]+)"`)
	if match := itunesRe.FindStringSubmatch(xmlContent); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}

	return ""
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
		PrivateFeed:  f.PrivateFeed,
		Config:       f,
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
		if dbFeed.PrivateFeed {
			summary.PrivateFeed = true
		}
	}

	// Always extract/override cover art from the generated XML (<image><url>...</url></image>)
	if xmlCover := m.extractCoverArtFromXML(f.ID); xmlCover != "" {
		summary.CoverArt = xmlCover
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
		switch ep.Status {
		case model.EpisodeDownloaded:
			dlCount++
			totalSize += ep.Size
		case model.EpisodeError:
			errCount++
		}
		return nil
	})

	summary.EpisodeCount = epCount
	summary.DownloadedCount = dlCount
	summary.ErrorCount = errCount
	summary.TotalSize = totalSize

	if errEntry, ok := m.feedErrors[f.ID]; ok {
		summary.LastError = errEntry.Error
		summary.LastErrorAt = &errEntry.Time
	}

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

	// Mount if audiobook
	m.mountAudiobookFeed(newFeed)

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

	// Unmount if mounted
	if mounter, ok := m.storage.(fs.Mounter); ok {
		mounter.Unmount(id)
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
		info, err := builder.ParseURL(f.URL)
		isAudiobook := (err == nil && info.Provider == model.ProviderAudiobook)
		if m.cfg.Storage.Type == "local" && !isAudiobook {
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

func (m *AppFeedManager) TriggerUpdateAll(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	feedList := make([]*feed.Config, 0, len(m.feeds))
	for _, f := range m.feeds {
		feedList = append(feedList, f)
	}
	m.mu.RUnlock()

	if len(feedList) == 0 {
		return []string{}, nil
	}

	var queued []string
	for _, f := range feedList {
		select {
		case m.updates <- f:
			log.Infof("manually triggered update for feed %q", f.ID)
			queued = append(queued, f.ID)
		default:
			log.Warnf("failed to queue update for feed %q: queue full", f.ID)
		}
	}

	return queued, nil
}

func (m *AppFeedManager) RetryEpisode(ctx context.Context, feedID, episodeID string) error {
	return m.db.UpdateEpisode(feedID, episodeID, func(ep *model.Episode) error {
		ep.Status = model.EpisodeNew
		ep.Error = ""
		ep.ErrorAt = nil
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
		{model.ProviderRutube, []string{"PODSYNC_RUTUBE_API_KEY"}},
	}

	result := make([]web.TokenInfo, 0, len(providers))
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

func (m *AppFeedManager) persistTokensLocked(providerStr string, newTokens []string) error {
	provider := model.Provider(providerStr)
	if m.tokens == nil {
		m.tokens = make(map[model.Provider]StringSlice)
	}

	if len(newTokens) > 0 {
		m.tokens[provider] = newTokens
	} else {
		delete(m.tokens, provider)
	}

	// Save to config.toml
	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err == nil {
			key := fmt.Sprintf("tokens.%s", providerStr)
			if len(newTokens) == 0 {
				tree.Delete(key)
			} else if len(newTokens) == 1 {
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

	return nil
}

func cleanToken(providerStr, tok string) string {
	tok = strings.TrimSpace(tok)
	tok = strings.Trim(tok, "\"'")
	if strings.EqualFold(providerStr, "vkvideo") || strings.EqualFold(providerStr, "vk") || strings.Contains(tok, "access_token=") {
		if idx := strings.Index(tok, "access_token="); idx != -1 {
			sub := tok[idx+len("access_token="):]
			if end := strings.IndexAny(sub, " &;\n\r#"); end != -1 {
				return sub[:end]
			}
			return sub
		}
	}
	return tok
}

func (m *AppFeedManager) UpdateTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newTokens []string
	for _, tok := range tokens {
		tok = cleanToken(providerStr, tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		newTokens = append(newTokens, tok)
	}

	return m.persistTokensLocked(providerStr, newTokens)
}

func (m *AppFeedManager) AddTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var validTokens []string
	for _, tok := range tokens {
		tok = cleanToken(providerStr, tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		validTokens = append(validTokens, tok)
	}
	if len(validTokens) == 0 {
		return nil
	}

	provider := model.Provider(providerStr)
	current := m.tokens[provider]
	combined := append([]string(current), validTokens...)

	return m.persistTokensLocked(providerStr, combined)
}

func (m *AppFeedManager) DeleteToken(ctx context.Context, providerStr string, index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 {
		return m.persistTokensLocked(providerStr, nil)
	}

	provider := model.Provider(providerStr)
	current := m.tokens[provider]
	if index >= len(current) {
		return fmt.Errorf("token index %d out of bounds (total tokens: %d)", index, len(current))
	}

	var remaining []string
	for i, tok := range current {
		if i != index {
			remaining = append(remaining, tok)
		}
	}

	return m.persistTokensLocked(providerStr, remaining)
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

func (m *AppFeedManager) UpdateFeed(ctx context.Context, id string, update *web.FeedConfigUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, exists := m.feeds[id]
	if !exists {
		return errors.Errorf("feed %q not found", id)
	}

	oldLang := strings.TrimSpace(f.Custom.Language)
	newLang := strings.TrimSpace(update.Custom.Language)
	if update.Language != "" && newLang == "" {
		newLang = strings.TrimSpace(update.Language)
		update.Custom.Language = newLang
	}
	languageChanged := (oldLang != newLang) && (oldLang != "" || newLang != "")
	shouldRedownload := update.Redownload || languageChanged

	if update.PageSize > 0 {
		f.PageSize = update.PageSize
	}
	if update.Format != "" {
		f.Format = model.Format(update.Format)
	}
	if update.Quality != "" {
		f.Quality = model.Quality(update.Quality)
	}
	f.MaxHeight = update.MaxHeight
	f.PlaylistSort = model.Sorting(update.PlaylistSort)
	if update.OPML != nil {
		f.OPML = update.OPML
	}
	f.PrivateFeed = update.PrivateFeed
	if m.cfg != nil && m.cfg.Feeds != nil {
		if cf, ok := m.cfg.Feeds[id]; ok {
			cf.PrivateFeed = update.PrivateFeed
		}
	}
	if m.db != nil {
		if dbFeed, err := m.db.GetFeed(ctx, id); err == nil && dbFeed != nil {
			dbFeed.PrivateFeed = f.PrivateFeed
			_ = m.db.AddFeed(ctx, id, dbFeed)
		}
	}

	if update.KeepLast > 0 {
		f.Clean = &feed.Cleanup{KeepLast: update.KeepLast}
	} else if update.KeepLast == 0 {
		f.Clean = nil
	}

	if update.Language != "" && update.Custom.Language == "" {
		update.Custom.Language = strings.TrimSpace(update.Language)
	}
	f.Filters = update.Filters
	f.Custom = update.Custom
	f.YouTubeDLArgs = update.YouTubeDLArgs
	f.FilenameTemplate = update.FilenameTemplate

	info, parseErr := builder.ParseURL(f.URL)
	if parseErr == nil && info.Provider == model.ProviderAudiobook {
		f.Clean = nil
		f.Format = model.FormatAudio
		f.Quality = model.QualityHigh
		f.MaxHeight = 0
		f.Filters = feed.Filters{}
		f.YouTubeDLArgs = nil
		f.PlaylistSort = ""
	}

	// Reschedule cron if cron_schedule changed
	if update.CronSchedule != "" && update.CronSchedule != f.CronSchedule {
		f.CronSchedule = update.CronSchedule
		if m.cron != nil {
			if oldID, ok := m.cronEntries[id]; ok {
				m.cron.Remove(oldID)
				delete(m.cronEntries, id)
			}
			feedToCron := f
			cronID, err := m.cron.AddFunc(f.CronSchedule, func() {
				log.Debugf("cron triggered for dynamic feed %q", feedToCron.ID)
				m.updates <- feedToCron
			})
			if err == nil {
				m.cronEntries[id] = cronID
			} else {
				log.WithError(err).Warnf("failed to re-register cron for feed %q", id)
			}
		}
	}

	if shouldRedownload && m.db != nil {
		log.Infof("redownload triggered for feed %q (language changed: %v, explicit redownload: %v, old: %q, new: %q)", id, languageChanged, update.Redownload, oldLang, newLang)
		_ = m.db.WalkEpisodes(ctx, id, func(ep *model.Episode) error {
			if ep.Status == model.EpisodeDownloaded {
				if m.storage != nil {
					_ = m.storage.Delete(ctx, fmt.Sprintf("%s/%s", id, feed.EpisodeName(f, ep)))
					_ = m.storage.Delete(ctx, fmt.Sprintf("%s/%s", id, feed.LegacyEpisodeName(f, ep)))
					for _, ext := range []string{"mp4", "mp3", "m4a", "webm"} {
						_ = m.storage.Delete(ctx, fmt.Sprintf("%s/%s.%s", id, ep.ID, ext))
					}
				}
				_ = m.db.UpdateEpisode(id, ep.ID, func(e *model.Episode) error {
					e.Status = model.EpisodeNew
					e.Size = 0
					e.Error = ""
					e.ErrorAt = nil
					return nil
				})
			}
			return nil
		})

		select {
		case m.updates <- f:
			log.Infof("queued immediate update for feed %q after redownload reset", id)
		default:
			log.Warnf("failed to queue immediate update for feed %q (queue full)", id)
		}
	}

	if err := m.saveFeedToConfig(f); err != nil {
		return err
	}

	if !shouldRedownload && m.updates != nil {
		select {
		case m.updates <- f:
			log.Infof("queued immediate update for feed %q after settings change", id)
		default:
			log.Warnf("failed to queue immediate update for feed %q (queue full)", id)
		}
	}

	return nil
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
	if f.Format != "" {
		tree.Set(prefix+".format", string(f.Format))
	}
	if f.Quality != "" {
		tree.Set(prefix+".quality", string(f.Quality))
	}
	if f.PageSize > 0 {
		tree.Set(prefix+".page_size", int64(f.PageSize))
	}
	if f.CronSchedule != "" {
		tree.Set(prefix+".cron_schedule", f.CronSchedule)
	}
	if f.MaxHeight > 0 {
		tree.Set(prefix+".max_height", int64(f.MaxHeight))
	} else {
		tree.Delete(prefix + ".max_height")
	}
	if f.PlaylistSort != "" {
		tree.Set(prefix+".playlist_sort", string(f.PlaylistSort))
	} else {
		tree.Delete(prefix + ".playlist_sort")
	}
	if f.OPML != nil {
		tree.Set(prefix+".opml", *f.OPML)
	}
	if f.PrivateFeed {
		tree.Set(prefix+".private_feed", true)
	} else {
		tree.Delete(prefix + ".private_feed")
	}
	if f.Clean != nil && f.Clean.KeepLast > 0 {
		tree.Set(prefix+".clean.keep_last", int64(f.Clean.KeepLast))
	} else {
		tree.Delete(prefix + ".clean")
	}

	// Filters
	if f.Filters.Title != "" {
		tree.Set(prefix+".filters.title", f.Filters.Title)
	} else {
		tree.Delete(prefix + ".filters.title")
	}
	if f.Filters.NotTitle != "" {
		tree.Set(prefix+".filters.not_title", f.Filters.NotTitle)
	} else {
		tree.Delete(prefix + ".filters.not_title")
	}
	if f.Filters.Description != "" {
		tree.Set(prefix+".filters.description", f.Filters.Description)
	} else {
		tree.Delete(prefix + ".filters.description")
	}
	if f.Filters.NotDescription != "" {
		tree.Set(prefix+".filters.not_description", f.Filters.NotDescription)
	} else {
		tree.Delete(prefix + ".filters.not_description")
	}
	if f.Filters.MinDuration > 0 {
		tree.Set(prefix+".filters.min_duration", f.Filters.MinDuration)
	} else {
		tree.Delete(prefix + ".filters.min_duration")
	}
	if f.Filters.MaxDuration > 0 {
		tree.Set(prefix+".filters.max_duration", f.Filters.MaxDuration)
	} else {
		tree.Delete(prefix + ".filters.max_duration")
	}
	if f.Filters.MaxAge > 0 {
		tree.Set(prefix+".filters.max_age", int64(f.Filters.MaxAge))
	} else {
		tree.Delete(prefix + ".filters.max_age")
	}

	// Custom
	if f.Custom.Title != "" {
		tree.Set(prefix+".custom.title", f.Custom.Title)
	} else {
		tree.Delete(prefix + ".custom.title")
	}
	if f.Custom.Description != "" {
		tree.Set(prefix+".custom.description", f.Custom.Description)
	} else {
		tree.Delete(prefix + ".custom.description")
	}
	if f.Custom.Author != "" {
		tree.Set(prefix+".custom.author", f.Custom.Author)
	} else {
		tree.Delete(prefix + ".custom.author")
	}
	if f.Custom.CoverArt != "" {
		tree.Set(prefix+".custom.cover_art", f.Custom.CoverArt)
	} else {
		tree.Delete(prefix + ".custom.cover_art")
	}
	if f.Custom.Category != "" {
		tree.Set(prefix+".custom.category", f.Custom.Category)
	} else {
		tree.Delete(prefix + ".custom.category")
	}
	if f.Custom.Language != "" {
		tree.Set(prefix+".custom.lang", f.Custom.Language)
	} else {
		tree.Delete(prefix + ".custom.lang")
	}
	if f.Custom.Explicit {
		tree.Set(prefix+".custom.explicit", true)
	} else {
		tree.Delete(prefix + ".custom.explicit")
	}

	// Advanced
	if len(f.YouTubeDLArgs) > 0 {
		tree.Set(prefix+".youtube_dl_args", f.YouTubeDLArgs)
	} else {
		tree.Delete(prefix + ".youtube_dl_args")
	}
	if f.FilenameTemplate != "" {
		tree.Set(prefix+".filename_template", f.FilenameTemplate)
	} else {
		tree.Delete(prefix + ".filename_template")
	}
	if f.AudiobookDir != "" {
		tree.Set(prefix+".audiobook_dir", f.AudiobookDir)
	} else {
		tree.Delete(prefix + ".audiobook_dir")
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

func getFFmpegVersion(ctx context.Context) (string, string) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ""
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, path, "-version")
	out, err := cmd.Output()
	if err != nil {
		return path, ""
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		fields := strings.Fields(firstLine)
		if len(fields) >= 3 && fields[0] == "ffmpeg" && fields[1] == "version" {
			return path, fields[2]
		}
		return path, strings.TrimPrefix(firstLine, "ffmpeg version ")
	}
	return path, ""
}

func (m *AppFeedManager) GetDownloaderConfig(ctx context.Context) (*web.DownloaderConfigInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var dlCfg ytdl.Config
	var binPath, binVer string
	ffmpegPath, ffmpegVer := getFFmpegVersion(ctx)

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
		Language:           dlCfg.Language,
		HasCookiesFile:     hasCookies,
		CookiesFileSize:    cookiesSize,
		CookiesFileLines:   cookiesLines,
		CookiesLastMod:     cookiesMod,
		BinaryPath:         binPath,
		BinaryVersion:      binVer,
		FFmpegPath:         ffmpegPath,
		FFmpegVersion:      ffmpegVer,
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
	if update.Language != nil {
		m.cfg.Downloader.Language = strings.TrimSpace(*update.Language)
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
		m.syncCookiesToFeeds("")
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

	m.syncCookiesToFeeds(cookiesPath)

	return m.saveDownloaderToConfig(&m.cfg.Downloader)
}

func (m *AppFeedManager) syncCookiesToFeeds(newCookiesPath string) {
	for feedID, feedCfg := range m.cfg.Feeds {
		if len(feedCfg.YouTubeDLArgs) == 0 {
			continue
		}
		hasCookies := false
		newArgs := make([]string, 0, len(feedCfg.YouTubeDLArgs))
		for i := 0; i < len(feedCfg.YouTubeDLArgs); i++ {
			arg := feedCfg.YouTubeDLArgs[i]
			if arg == "--cookies" {
				if i+1 < len(feedCfg.YouTubeDLArgs) {
					if newCookiesPath != "" {
						newArgs = append(newArgs, "--cookies", newCookiesPath)
					}
					i++ // skip old path argument
					hasCookies = true
					continue
				}
			} else if strings.HasPrefix(arg, "--cookies=") {
				if newCookiesPath != "" {
					newArgs = append(newArgs, "--cookies="+newCookiesPath)
				}
				hasCookies = true
				continue
			}
			newArgs = append(newArgs, arg)
		}

		if hasCookies {
			feedCfg.YouTubeDLArgs = newArgs
			m.cfg.Feeds[feedID] = feedCfg
			_ = m.saveFeedToConfig(feedCfg)
			log.Infof("updated --cookies path in feed %q to %q", feedID, newCookiesPath)
		}
	}
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
	if cfg.Language != "" {
		tree.Set("downloader.language", cfg.Language)
	} else {
		tree.Delete("downloader.language")
	}

	f, err := os.Create(m.configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = tree.WriteTo(f)
	return err
}

func (m *AppFeedManager) GetDownloadErrors(ctx context.Context, targetFeedID string, limit int) ([]web.DownloadErrorItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	var items []web.DownloadErrorItem

	collectFromFeed := func(feedConfig *feed.Config) {
		feedTitle := feedConfig.ID
		provider := ""
		if dbFeed, err := m.db.GetFeed(ctx, feedConfig.ID); err == nil && dbFeed != nil {
			if dbFeed.Title != "" {
				feedTitle = dbFeed.Title
			}
			provider = string(dbFeed.Provider)
		}
		if provider == "" {
			if info, err := builder.ParseURL(feedConfig.URL); err == nil {
				provider = string(info.Provider)
			}
		}

		_ = m.db.WalkEpisodes(ctx, feedConfig.ID, func(ep *model.Episode) error {
			if ep.Status == model.EpisodeError {
				rec := ""
				errStr := ep.Error
				if strings.Contains(errStr, "bot") || strings.Contains(errStr, "cookies") || strings.Contains(errStr, "Sign in") {
					rec = "Configure Netscape cookies.txt file in Web Admin or via PODSYNC_DOWNLOADER_COOKIES_FILE to bypass bot detection"
				} else if strings.Contains(errStr, "429") || strings.Contains(errStr, "Too Many Requests") {
					rec = "Rate limited by source platform; use a proxy or reduce update frequency"
				} else if strings.Contains(errStr, "Private") {
					rec = "Video is private; authentication cookies for the video owner are required"
				} else if strings.Contains(errStr, "timed out") {
					rec = "Download timed out; check network bandwidth or increase downloader timeout"
				}

				item := web.DownloadErrorItem{
					FeedID:         feedConfig.ID,
					FeedTitle:      feedTitle,
					FeedProvider:   provider,
					EpisodeID:      ep.ID,
					EpisodeTitle:   ep.Title,
					VideoURL:       ep.VideoURL,
					PubDate:        ep.PubDate,
					Error:          errStr,
					ErrorAt:        ep.ErrorAt,
					Recommendation: rec,
				}
				if item.Error == "" {
					item.Error = "Download failed (error exit status)"
				}
				items = append(items, item)
			}
			return nil
		})

		if feedErr, hasErr := m.feedErrors[feedConfig.ID]; hasErr {
			rec := "Check logs or API configuration"
			errLower := strings.ToLower(feedErr.Error)
			if strings.Contains(errLower, "token is required") || strings.Contains(errLower, "not loaded") || strings.Contains(errLower, "api token") {
				rec = "Add the required API token for " + provider + " in the API Tokens tab"
			}
			errItem := web.DownloadErrorItem{
				FeedID:         feedConfig.ID,
				FeedTitle:      feedTitle,
				FeedProvider:   provider,
				EpisodeID:      "feed_update",
				EpisodeTitle:   "Feed Update Failure",
				VideoURL:       feedConfig.URL,
				Error:          feedErr.Error,
				ErrorAt:        &feedErr.Time,
				Recommendation: rec,
			}
			items = append(items, errItem)
		}
	}

	if targetFeedID != "" {
		if f, ok := m.feeds[targetFeedID]; ok {
			collectFromFeed(f)
		}
	} else {
		for _, f := range m.feeds {
			collectFromFeed(f)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ErrorAt != nil && items[j].ErrorAt != nil {
			return items[i].ErrorAt.After(*items[j].ErrorAt)
		}
		return items[i].PubDate.After(items[j].PubDate)
	})

	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

func (m *AppFeedManager) GetRecentDownloads(ctx context.Context, targetFeedID string, limit int) ([]web.RecentDownloadItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	} else if limit > 100 {
		limit = 100
	}

	var items []web.RecentDownloadItem

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

	collectFromFeed := func(feedConfig *feed.Config) {
		feedTitle := feedConfig.ID
		provider := ""
		if dbFeed, err := m.db.GetFeed(ctx, feedConfig.ID); err == nil && dbFeed != nil {
			if dbFeed.Title != "" {
				feedTitle = dbFeed.Title
			}
			provider = string(dbFeed.Provider)
		}
		if provider == "" {
			if info, err := builder.ParseURL(feedConfig.URL); err == nil {
				provider = string(info.Provider)
			}
		}

		_ = m.db.WalkEpisodes(ctx, feedConfig.ID, func(ep *model.Episode) error {
			if ep.Status == model.EpisodeDownloaded {
				epName := feed.EpisodeName(feedConfig, ep)
				mediaURL := fmt.Sprintf("%s%s/%s/%s", host, mountPath, feedConfig.ID, epName)
				items = append(items, web.RecentDownloadItem{
					FeedID:       feedConfig.ID,
					FeedTitle:    feedTitle,
					FeedProvider: provider,
					EpisodeID:    ep.ID,
					EpisodeTitle: ep.Title,
					VideoURL:     ep.VideoURL,
					PubDate:      ep.PubDate,
					Size:         ep.Size,
					Duration:     ep.Duration,
					MediaURL:     mediaURL,
				})
			}
			return nil
		})
	}

	if targetFeedID != "" {
		if f, ok := m.feeds[targetFeedID]; ok {
			collectFromFeed(f)
		}
	} else {
		for _, f := range m.feeds {
			collectFromFeed(f)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].PubDate.After(items[j].PubDate)
	})

	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

func (m *AppFeedManager) GetSyncStatus(ctx context.Context) (*web.SyncStatusInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := &web.SyncStatusInfo{
		ActiveFeeds: len(m.feeds),
		Feeds:       make([]web.SyncStatusFeedInfo, 0, len(m.feeds)),
	}

	// Binary presence
	if m.downloader != nil {
		info.HasDownloader = true
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		info.HasFFmpeg = true
	}

	// Disk stats
	if stats, err := m.GetSystemStats(ctx); err == nil && stats != nil {
		info.DiskFreeBytes = stats.DiskFree
		info.DiskTotalBytes = stats.DiskTotal
	}

	past24h := time.Now().Add(-24 * time.Hour)

	for _, f := range m.feeds {
		feedTitle := f.ID
		provider := ""
		var lastUpdate *time.Time
		if dbFeed, err := m.db.GetFeed(ctx, f.ID); err == nil && dbFeed != nil {
			if dbFeed.Title != "" {
				feedTitle = dbFeed.Title
			}
			provider = string(dbFeed.Provider)
			if !dbFeed.UpdatedAt.IsZero() {
				lastUpdate = &dbFeed.UpdatedAt
			}
		}
		if provider == "" {
			if pInfo, err := builder.ParseURL(f.URL); err == nil {
				provider = string(pInfo.Provider)
			}
		}

		var nextUpdate *time.Time
		if m.cron != nil {
			if entryID, exists := m.cronEntries[f.ID]; exists {
				nextTime := m.cron.Entry(entryID).Next
				if !nextTime.IsZero() {
					nextUpdate = &nextTime
				}
			}
		}

		var epCount, dlCount, errCount int
		_ = m.db.WalkEpisodes(ctx, f.ID, func(ep *model.Episode) error {
			epCount++
			switch ep.Status {
			case model.EpisodeDownloaded:
				dlCount++
				if ep.PubDate.After(past24h) {
					info.DownloadedPast24h++
				}
			case model.EpisodeError:
				errCount++
			}
			return nil
		})

		info.TotalEpisodes += epCount
		info.DownloadedEpisodes += dlCount
		info.FailedEpisodes += errCount

		info.Feeds = append(info.Feeds, web.SyncStatusFeedInfo{
			FeedID:          f.ID,
			FeedTitle:       feedTitle,
			URL:             f.URL,
			Provider:        provider,
			CronSchedule:    f.CronSchedule,
			LastUpdate:      lastUpdate,
			NextUpdate:      nextUpdate,
			EpisodeCount:    epCount,
			DownloadedCount: dlCount,
			ErrorCount:      errCount,
		})
	}

	sort.Slice(info.Feeds, func(i, j int) bool {
		return info.Feeds[i].FeedID < info.Feeds[j].FeedID
	})

	return info, nil
}

func (m *AppFeedManager) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	currentPassword := m.cfg.Server.Admin.Password
	if currentPassword != "" && currentPassword != oldPassword {
		return errors.New("current password does not match")
	}

	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new password cannot be empty")
	}
	if len(newPassword) < 4 {
		return errors.New("new password must be at least 4 characters long")
	}

	m.cfg.Server.Admin.Password = newPassword

	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err != nil {
			return errors.Wrap(err, "failed to load config file")
		}
		tree.Set("server.admin.password", newPassword)

		f, err := os.Create(m.configPath)
		if err != nil {
			return errors.Wrap(err, "failed to write config file")
		}
		defer f.Close()

		if _, err := tree.WriteTo(f); err != nil {
			return errors.Wrap(err, "failed to save config file")
		}
	}

	return nil
}

func (m *AppFeedManager) RecordFeedUpdateError(feedID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.feedErrors == nil {
		m.feedErrors = make(map[string]feedErrorEntry)
	}
	if err != nil {
		m.feedErrors[feedID] = feedErrorEntry{
			Error: err.Error(),
			Time:  time.Now(),
		}
	} else {
		delete(m.feedErrors, feedID)
	}
}

func (m *AppFeedManager) ClearFeedUpdateError(feedID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.feedErrors != nil {
		delete(m.feedErrors, feedID)
	}
}

func (m *AppFeedManager) GetSystemLogs(ctx context.Context, filter web.LogFilter) (*web.LogResult, error) {
	if m.logBuffer == nil {
		return &web.LogResult{
			CurrentLevel: log.GetLevel().String(),
		}, nil
	}

	res := m.logBuffer.Query(logbuffer.Filter{
		Level:   filter.Level,
		Search:  filter.Search,
		FeedID:  filter.FeedID,
		Limit:   filter.Limit,
		SinceID: filter.SinceID,
	})

	entries := make([]web.LogEntry, len(res.Entries))
	for i, e := range res.Entries {
		entries[i] = web.LogEntry{
			ID:        e.ID,
			Timestamp: e.Timestamp,
			Level:     e.Level,
			Message:   e.Message,
			Fields:    e.Fields,
			Caller:    e.Caller,
		}
	}

	return &web.LogResult{
		Entries:       entries,
		Total:         res.Total,
		FilteredTotal: res.FilteredTotal,
		MaxID:         res.MaxID,
		CurrentLevel:  res.CurrentLevel,
	}, nil
}

func (m *AppFeedManager) ClearSystemLogs(ctx context.Context) error {
	if m.logBuffer != nil {
		m.logBuffer.Clear()
	}
	return nil
}

func (m *AppFeedManager) SetLogLevel(ctx context.Context, levelStr string) (string, error) {
	lvl, err := log.ParseLevel(levelStr)
	if err != nil {
		return "", errors.Wrapf(err, "invalid log level %q", levelStr)
	}
	log.SetLevel(lvl)
	log.Infof("system log level dynamically set to %s", lvl.String())
	return lvl.String(), nil
}

func (m *AppFeedManager) GetLogLevel(ctx context.Context) string {
	return log.GetLevel().String()
}

func (m *AppFeedManager) DownloadLogs(ctx context.Context) ([]byte, string, error) {
	if m.cfg != nil && m.cfg.Log.Filename != "" {
		if data, err := os.ReadFile(m.cfg.Log.Filename); err == nil && len(data) > 0 {
			filename := filepath.Base(m.cfg.Log.Filename)
			return data, filename, nil
		}
	}

	if m.logBuffer != nil {
		content := m.logBuffer.FormatPlainText(logbuffer.Filter{Limit: 2000})
		return []byte(content), "podsync.log", nil
	}

	return []byte("No logs available\n"), "podsync.log", nil
}

func (m *AppFeedManager) GetAudiobooksDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg != nil && m.cfg.Audiobooks.Dir != "" {
		return m.cfg.Audiobooks.Dir
	}
	dir := os.Getenv("PODSYNC_AUDIOBOOKS_DIR")
	if dir != "" {
		return dir
	}
	return "./audiobooks"
}

func (m *AppFeedManager) ListAudiobookDirs(ctx context.Context) ([]web.AudiobookDirInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	baseDir := m.GetAudiobooksDir()
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, errors.Wrapf(err, "failed to access audiobooks directory: %s", baseDir)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read audiobooks directory: %s", baseDir)
	}

	exts := map[string]bool{
		".mp3": true, ".m4b": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".flac": true,
	}
	if m.cfg != nil && len(m.cfg.Audiobooks.SupportedExtensions) > 0 {
		exts = make(map[string]bool)
		for _, e := range m.cfg.Audiobooks.SupportedExtensions {
			exts[strings.ToLower(e)] = true
		}
	}

	coverNames := map[string]bool{
		"cover.jpg": true, "cover.png": true, "folder.jpg": true, "cover.jpeg": true,
	}
	if m.cfg != nil && len(m.cfg.Audiobooks.CoverFilenames) > 0 {
		coverNames = make(map[string]bool)
		for _, c := range m.cfg.Audiobooks.CoverFilenames {
			coverNames[strings.ToLower(c)] = true
		}
	}

	// Map existing feeds for quick lookup
	linkedDirs := make(map[string]string)
	for _, f := range m.feeds {
		info, err := builder.ParseURL(f.URL)
		if err == nil && info.Provider == model.ProviderAudiobook {
			linkedDirs[info.ItemID] = f.ID
			linkedDirs[filepath.Base(info.ItemID)] = f.ID
		}
	}

	results := make([]web.AudiobookDirInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		if strings.HasPrefix(dirName, ".") {
			continue
		}

		fullPath := filepath.Join(baseDir, dirName)
		files, err := os.ReadDir(fullPath)
		if err != nil {
			continue
		}

		fileCount := 0
		var totalSize int64
		hasCover := false

		for _, f := range files {
			if f.IsDir() {
				continue
			}
			lowerName := strings.ToLower(f.Name())
			if coverNames[lowerName] {
				hasCover = true
			}
			ext := strings.ToLower(filepath.Ext(lowerName))
			if exts[ext] {
				fileCount++
				if info, err := f.Info(); err == nil {
					totalSize += info.Size()
				}
			}
		}

		feedID, isLinked := linkedDirs[dirName]

		var suggestedAuthor, suggestedTitle string
		if parts := strings.SplitN(dirName, " - ", 2); len(parts) == 2 {
			suggestedAuthor = strings.TrimSpace(parts[0])
			suggestedTitle = strings.TrimSpace(parts[1])
		} else {
			suggestedTitle = dirName
		}

		results = append(results, web.AudiobookDirInfo{
			Name:            dirName,
			Path:            fullPath,
			FileCount:       fileCount,
			HasCover:        hasCover,
			TotalSize:       totalSize,
			Linked:          isLinked,
			FeedID:          feedID,
			SuggestedTitle:  suggestedTitle,
			SuggestedAuthor: suggestedAuthor,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	return results, nil
}

func generateAudiobookSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	reg := regexp.MustCompile(`[^a-z0-9_-]+`)
	slug := reg.ReplaceAllString(s, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.Trim(slug, "-")
	}
	return slug
}

func (m *AppFeedManager) ImportAudiobook(ctx context.Context, req *web.ImportAudiobookRequest) (*feed.Config, error) {
	if req == nil {
		return nil, errors.New("empty request")
	}
	dirName := strings.TrimSpace(req.DirName)
	if dirName == "" {
		return nil, errors.New("dir_name is required")
	}

	cleanedDir := filepath.Clean(dirName)
	if strings.HasPrefix(cleanedDir, "..") || strings.Contains(cleanedDir, string(filepath.Separator)+"..") {
		return nil, errors.New("invalid directory path")
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		base := req.Title
		if base == "" {
			base = dirName
		}
		id = generateAudiobookSlug(base)
	}

	if id == "" {
		return nil, errors.New("could not generate valid feed ID")
	}

	m.mu.RLock()
	_, exists := m.feeds[id]
	m.mu.RUnlock()
	if exists {
		return nil, errors.Errorf("feed with ID %q already exists", id)
	}

	baseDir := m.GetAudiobooksDir()
	fullPath := filepath.Join(baseDir, cleanedDir)
	if stat, err := os.Stat(fullPath); err != nil || !stat.IsDir() {
		if stat, err := os.Stat(cleanedDir); err == nil && stat.IsDir() {
			fullPath = cleanedDir
		} else {
			return nil, errors.Errorf("audiobook directory %q does not exist", fullPath)
		}
	}

	schedule := req.CronSchedule
	if schedule == "" {
		schedule = "@every 12h"
	}

	newFeed := &feed.Config{
		ID:           id,
		URL:          fmt.Sprintf("audiobook://%s", cleanedDir),
		Format:       model.FormatCustom,
		PlaylistSort: model.SortingAsc,
		PageSize:     1000,
		CronSchedule: schedule,
		AudiobookDir: fullPath,
	}

	if req.Title != "" || req.Author != "" || req.Description != "" {
		newFeed.Custom = feed.Custom{
			Title:       req.Title,
			Author:      req.Author,
			Description: req.Description,
		}
	}
	if req.PrivateFeed {
		newFeed.PrivateFeed = true
	}

	if err := m.AddFeed(ctx, newFeed); err != nil {
		return nil, err
	}

	return newFeed, nil
}

func (m *AppFeedManager) SaveAudiobookFile(bookDir string, filename string, r io.Reader) error {
	baseDir := m.GetAudiobooksDir()
	cleanBookDir := filepath.Clean(bookDir)
	if strings.HasPrefix(cleanBookDir, "..") || strings.Contains(cleanBookDir, string(filepath.Separator)+"..") {
		return errors.New("invalid book directory")
	}
	targetDir := filepath.Join(baseDir, cleanBookDir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create book directory")
	}

	cleanFilename := filepath.Base(filepath.Clean(filename))
	if cleanFilename == "." || cleanFilename == "/" || cleanFilename == ".." {
		return errors.New("invalid filename")
	}

	targetPath := filepath.Join(targetDir, cleanFilename)
	out, err := os.Create(targetPath)
	if err != nil {
		return errors.Wrap(err, "failed to create destination file")
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return errors.Wrap(err, "failed to write file")
	}
	return nil
}

func (m *AppFeedManager) DeleteAudiobookDir(ctx context.Context, dirName string) error {
	dirName = strings.TrimSpace(dirName)
	if dirName == "" {
		return errors.New("dir_name is required")
	}

	cleanDir := filepath.Clean(dirName)
	if cleanDir == "." || cleanDir == "/" || strings.HasPrefix(cleanDir, "..") || strings.Contains(cleanDir, string(filepath.Separator)+"..") {
		return errors.New("invalid directory path")
	}

	baseDir := m.GetAudiobooksDir()
	if baseDir == "" {
		return errors.New("audiobooks directory is not configured")
	}

	targetPath := filepath.Join(baseDir, cleanDir)
	stat, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.Errorf("audiobook directory %q not found", dirName)
		}
		return err
	}
	if !stat.IsDir() {
		return errors.Errorf("%q is not a directory", dirName)
	}

	// Security: verify targetPath is strictly inside baseDir
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return errors.New("cannot delete outside of audiobooks directory")
	}

	// If this directory is linked to any active feed(s), delete the feed(s) cleanly
	m.mu.Lock()
	var linkedFeedIDs []string
	for feedID, f := range m.feeds {
		info, parseErr := builder.ParseURL(f.URL)
		if parseErr == nil && info.Provider == model.ProviderAudiobook && (info.ItemID == cleanDir || f.AudiobookDir == targetPath) {
			linkedFeedIDs = append(linkedFeedIDs, feedID)
		}
	}
	m.mu.Unlock()

	for _, feedID := range linkedFeedIDs {
		log.Infof("deleting linked audiobook feed %q before deleting folder %q", feedID, dirName)
		if delErr := m.DeleteFeed(ctx, feedID, true); delErr != nil {
			log.WithError(delErr).Warnf("failed to delete linked feed %q during folder deletion", feedID)
		}
	}

	// Remove folder from disk
	if err := os.RemoveAll(targetPath); err != nil {
		return errors.Wrapf(err, "failed to delete audiobook directory %q", targetPath)
	}

	log.Infof("successfully deleted audiobook directory from disk: %s", targetPath)
	return nil
}
