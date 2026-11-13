package main

// Runtime configuration: system metrics, downloader, network, cookies and
// native self-update.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/network"
	"github.com/mxpv/podsync/pkg/selfupdate"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/web"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func (m *AppFeedManager) GetSystemStats(ctx context.Context) (*web.SystemStats, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	m.mu.RLock()
	version := m.version
	commit := m.commit
	buildDate := m.buildDate
	startedAt := m.startedAt
	var dataDir, storageType string
	if m.cfg != nil {
		dataDir = m.cfg.Storage.Local.DataDir
		storageType = m.cfg.Storage.Type
	}
	m.mu.RUnlock()

	total, free, used, _ := getDiskSpace(dataDir)

	_, hasYtDlp := exec.LookPath("yt-dlp")
	_, hasFFmpeg := exec.LookPath("ffmpeg")

	stats := &web.SystemStats{
		Version:    version,
		Commit:     commit,
		BuildDate:  buildDate,
		Uptime:     time.Since(startedAt).Truncate(time.Second).String(),
		StartedAt:  startedAt,
		Goroutines: runtime.NumGoroutine(),
		MemoryMB:   float64(mem.Alloc) / 1024 / 1024,
		DiskTotal:  total,
		DiskFree:   free,
		DiskUsed:   used,
		HasYtDlp:   hasYtDlp == nil,
		HasFFmpeg:  hasFFmpeg == nil,
		DataDir:    dataDir,
		Storage:    storageType,
	}

	return stats, nil
}

// SetSelfUpdater enables native binary updates from the official GitHub release.

// SetSelfUpdater enables native binary updates from the official GitHub release.
func (m *AppFeedManager) SetSelfUpdater(updater *selfupdate.Updater, restart func() error) {
	m.mu.Lock()
	m.selfUpdater = updater
	m.restart = restart
	m.mu.Unlock()
}

func (m *AppFeedManager) GetPodsyncUpdate(ctx context.Context) (*web.PodsyncUpdateInfo, error) {
	m.mu.RLock()
	updater := m.selfUpdater
	m.mu.RUnlock()
	if updater == nil {
		return nil, errors.New("Podsync self-update is unavailable because the executable path could not be determined")
	}
	status, err := updater.Status(ctx)
	if err != nil {
		return nil, err
	}
	return podsyncUpdateInfo(status), nil
}

func (m *AppFeedManager) ApplyPodsyncUpdate(ctx context.Context) (*web.PodsyncUpdateInfo, error) {
	m.selfUpdateMu.Lock()
	if m.selfUpdating {
		m.selfUpdateMu.Unlock()
		return nil, errors.New("a Podsync update is already in progress")
	}
	m.selfUpdating = true
	m.selfUpdateMu.Unlock()
	defer func() {
		m.selfUpdateMu.Lock()
		m.selfUpdating = false
		m.selfUpdateMu.Unlock()
	}()

	m.mu.RLock()
	updater := m.selfUpdater
	restart := m.restart
	m.mu.RUnlock()
	if updater == nil || restart == nil {
		return nil, errors.New("Podsync self-update is unavailable because the executable path could not be determined")
	}
	status, err := updater.Apply(ctx)
	if err != nil {
		return podsyncUpdateInfo(status), err
	}
	info := podsyncUpdateInfo(status)
	info.Message = fmt.Sprintf("Podsync %s was installed and will restart now.", status.LatestVersion)
	go func() {
		time.Sleep(750 * time.Millisecond)
		if err := restart(); err != nil {
			log.WithError(err).Error("failed to restart Podsync after update")
		}
	}()
	return info, nil
}

func podsyncUpdateInfo(status selfupdate.Status) *web.PodsyncUpdateInfo {
	return &web.PodsyncUpdateInfo{
		CurrentVersion:  status.CurrentVersion,
		LatestVersion:   status.LatestVersion,
		ReleaseURL:      status.ReleaseURL,
		UpdateAvailable: status.UpdateAvailable,
		CanUpdate:       status.CanUpdate,
		Container:       status.Container,
		Message:         status.Message,
	}
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
	dl := m.downloader
	var dlCfg ytdl.Config
	if m.cfg != nil {
		dlCfg = m.cfg.Downloader
	}
	var dataDir, storageType string
	if m.cfg != nil {
		dataDir = m.cfg.Storage.Local.DataDir
		storageType = m.cfg.Storage.Type
	}
	m.mu.RUnlock()

	var binPath, binVer string
	ffmpegPath, ffmpegVer := getFFmpegVersion(ctx)

	if dl != nil {
		dlCfg = dl.GetConfig()
		binPath = dl.GetBinaryPath()
		verCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if ver, err := dl.GetVersion(verCtx); err == nil {
			binVer = strings.TrimSpace(ver)
		}
		cancel()
	}

	cookiesPath := dlCfg.CookiesFile
	if cookiesPath == "" && storageType == "local" && dataDir != "" {
		defaultPath := filepath.Join(dataDir, "cookies.txt")
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
		Proxies:            dlCfg.Proxies,
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
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	m.mu.RLock()
	cfgCopy := m.cfg.Downloader
	m.mu.RUnlock()
	if update.Timeout != nil && *update.Timeout > 0 {
		cfgCopy.Timeout = *update.Timeout
	}
	if update.SelfUpdate != nil {
		cfgCopy.SelfUpdate = *update.SelfUpdate
	}
	if update.CustomBinary != nil {
		cfgCopy.CustomBinary = *update.CustomBinary
	}
	if update.CookiesFile != nil {
		cfgCopy.CookiesFile = *update.CookiesFile
	}
	if update.CookiesFromBrowser != nil {
		cfgCopy.CookiesFromBrowser = *update.CookiesFromBrowser
	}
	if update.Proxy != nil {
		cfgCopy.Proxy = *update.Proxy
	}
	if update.Proxies != nil {
		cfgCopy.Proxies = *update.Proxies
	}
	if update.Language != nil {
		cfgCopy.Language = strings.TrimSpace(*update.Language)
	}

	if err := m.saveDownloaderToConfig(&cfgCopy); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg.Downloader = cfgCopy
	if m.downloader != nil {
		m.downloader.UpdateConfig(cfgCopy)
	}
	m.mu.Unlock()
	return nil
}

func (m *AppFeedManager) GetNetworkConfig(ctx context.Context) (*web.NetworkConfigInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil {
		return &web.NetworkConfigInfo{}, nil
	}
	return &web.NetworkConfigInfo{DNSServers: append([]string(nil), m.cfg.Network.DNSServers...)}, nil
}

func (m *AppFeedManager) UpdateNetworkConfig(ctx context.Context, update *web.NetworkConfigUpdate) error {
	if update == nil || update.DNSServers == nil {
		return errors.New("dns_servers is required")
	}
	servers := make([]string, 0, len(*update.DNSServers))
	for _, server := range *update.DNSServers {
		server = strings.TrimSpace(server)
		if server != "" {
			servers = append(servers, server)
		}
	}
	if err := network.ValidateDNSServers(servers); err != nil {
		return err
	}

	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()
	if err := m.saveNetworkToConfig(servers); err != nil {
		return err
	}
	if m.updater != nil {
		if err := m.updater.SetDNSServers(servers); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if m.cfg != nil {
		m.cfg.Network.DNSServers = append([]string(nil), servers...)
	}
	m.mu.Unlock()
	return nil
}

func (m *AppFeedManager) saveNetworkToConfig(servers []string) error {
	if m.configPath == "" {
		return nil
	}
	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		tree.Delete("network.dns_servers")
	} else {
		tree.Set("network.dns_servers", servers)
	}
	return writeConfigTree(m.configPath, tree)
}

func (m *AppFeedManager) GetCookiesContent(ctx context.Context) (string, error) {
	m.mu.RLock()
	cookiesPath := ""
	if m.cfg != nil {
		cookiesPath = m.cfg.Downloader.CookiesFile
		if cookiesPath == "" && m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
			cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		}
	}
	m.mu.RUnlock()

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
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	m.mu.RLock()
	cookiesPath := ""
	if m.cfg != nil {
		cookiesPath = m.cfg.Downloader.CookiesFile
		if cookiesPath == "" && m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
			cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		} else if cookiesPath == "" {
			cookiesPath = "cookies.txt"
		}
	}
	m.mu.RUnlock()

	content = strings.TrimSpace(content)
	if content == "" {
		// Remove cookies file
		_ = os.Remove(cookiesPath)
		m.mu.Lock()
		m.cfg.Downloader.CookiesFile = ""
		if m.downloader != nil {
			m.downloader.SetCookiesFile("")
		}
		cfgCopy := m.cfg.Downloader
		m.mu.Unlock()
		if err := m.syncCookiesToFeeds(""); err != nil {
			return err
		}
		return m.saveDownloaderToConfig(&cfgCopy)
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

	m.mu.Lock()
	m.cfg.Downloader.CookiesFile = cookiesPath
	if m.downloader != nil {
		m.downloader.SetCookiesFile(cookiesPath)
	}

	cfgCopy := m.cfg.Downloader
	m.mu.Unlock()
	if err := m.syncCookiesToFeeds(cookiesPath); err != nil {
		return err
	}

	return m.saveDownloaderToConfig(&cfgCopy)
}

func (m *AppFeedManager) syncCookiesToFeeds(newCookiesPath string) error {
	m.mu.RLock()
	feeds := make(map[string]*feed.Config, len(m.cfg.Feeds))
	for id, cfg := range m.cfg.Feeds {
		copy := *cfg
		feeds[id] = &copy
	}
	m.mu.RUnlock()
	for feedID, feedCfg := range feeds {
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
			if err := m.saveFeedToConfig(feedCfg); err != nil {
				return err
			}
			m.mu.Lock()
			m.cfg.Feeds[feedID] = feedCfg
			if m.feeds != nil {
				m.feeds[feedID] = feedCfg
			}
			if m.updater != nil {
				m.updater.SetFeedConfig(feedID, feedCfg)
			}
			m.mu.Unlock()
			log.Infof("updated --cookies path in feed %q to %q", feedID, newCookiesPath)
		}
	}
	return nil
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
	if len(cfg.Proxies) > 0 {
		for k, v := range cfg.Proxies {
			if v != "" {
				tree.Set("downloader.proxies."+k, v)
			} else {
				tree.Delete("downloader.proxies." + k)
			}
		}
	} else {
		tree.Delete("downloader.proxies")
	}
	if cfg.Language != "" {
		tree.Set("downloader.language", cfg.Language)
	} else {
		tree.Delete("downloader.language")
	}

	return writeConfigTree(m.configPath, tree)
}
