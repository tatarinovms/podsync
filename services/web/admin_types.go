package web

import (
	"context"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

// AdminConfig holds configuration for the admin web panel
type AdminConfig struct {
	Enabled    bool   `toml:"enabled"`
	Username   string `toml:"username"`
	Password   string `toml:"password"`
	ConfigPath string `toml:"-"`
}

// FeedSummary contains high-level information about a feed formatted for the admin UI
type FeedSummary struct {
	ID              string     `json:"id"`
	URL             string     `json:"url"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Author          string     `json:"author"`
	CoverArt        string     `json:"cover_art"`
	Provider        string     `json:"provider"`
	Format          string     `json:"format"`
	Quality         string     `json:"quality"`
	PageSize        int        `json:"page_size"`
	CronSchedule    string     `json:"cron_schedule"`
	UpdatePeriod    string     `json:"update_period"`
	EpisodeCount    int        `json:"episode_count"`
	DownloadedCount int        `json:"downloaded_count"`
	ErrorCount      int        `json:"error_count"`
	TotalSize       int64      `json:"total_size"`
	LastUpdate      *time.Time `json:"last_update,omitempty"`
	FeedURL         string     `json:"feed_url"`
	AppleURL        string     `json:"apple_url"`
	PocketCastsURL  string     `json:"pocketcasts_url"`
	OvercastURL     string     `json:"overcast_url"`
}

// SystemStats contains runtime statistics and server metrics
type SystemStats struct {
	Version    string    `json:"version"`
	Commit     string    `json:"commit"`
	BuildDate  string    `json:"build_date"`
	Uptime     string    `json:"uptime"`
	StartedAt  time.Time `json:"started_at"`
	Goroutines int       `json:"goroutines"`
	MemoryMB   float64   `json:"memory_mb"`
	DiskTotal  uint64    `json:"disk_total_bytes"`
	DiskFree   uint64    `json:"disk_free_bytes"`
	DiskUsed   uint64    `json:"disk_used_bytes"`
	HasYtDlp   bool      `json:"has_ytdlp"`
	HasFFmpeg  bool      `json:"has_ffmpeg"`
	DataDir    string    `json:"data_dir"`
	Storage    string    `json:"storage_type"`
}

// TokenInfo represents a provider's configured tokens
type TokenInfo struct {
	Provider string   `json:"provider"`
	Tokens   []string `json:"tokens"` // Masked tokens for security
	FromEnv  bool     `json:"from_env"`
}

// DownloaderConfigInfo contains current yt-dlp / downloader settings and cookie status
type DownloaderConfigInfo struct {
	Timeout            int    `json:"timeout"`
	SelfUpdate         bool   `json:"self_update"`
	CustomBinary       string `json:"custom_binary"`
	CookiesFile        string `json:"cookies_file"`
	CookiesFromBrowser string `json:"cookies_from_browser"`
	Proxy              string `json:"proxy"`
	HasCookiesFile     bool   `json:"has_cookies_file"`
	CookiesFileSize    int64  `json:"cookies_file_size"`
	CookiesFileLines   int    `json:"cookies_file_lines"`
	CookiesLastMod     string `json:"cookies_last_modified"`
	BinaryPath         string `json:"binary_path"`
	BinaryVersion      string `json:"binary_version"`
}

// DownloaderConfigUpdate contains fields to update downloader settings
type DownloaderConfigUpdate struct {
	Timeout            *int    `json:"timeout"`
	SelfUpdate         *bool   `json:"self_update"`
	CustomBinary       *string `json:"custom_binary"`
	CookiesFile        *string `json:"cookies_file"`
	CookiesFromBrowser *string `json:"cookies_from_browser"`
	Proxy              *string `json:"proxy"`
}

// CookiesPayload holds cookies file content
type CookiesPayload struct {
	Content string `json:"content"`
}

// TestDownloaderResult holds the test execution outcome
type TestDownloaderResult struct {
	Success bool   `json:"success"`
	Title   string `json:"title,omitempty"`
	Channel string `json:"channel,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// AdminManager is the interface required by the web admin handlers
type AdminManager interface {
	ListFeeds(ctx context.Context) ([]FeedSummary, error)
	GetFeedDetail(ctx context.Context, id string) (*FeedSummary, []*model.Episode, error)
	AddFeed(ctx context.Context, cfg *feed.Config) error
	DeleteFeed(ctx context.Context, id string, deleteFiles bool) error
	TriggerUpdate(ctx context.Context, id string) error
	RetryEpisode(ctx context.Context, feedID, episodeID string) error
	GetTokens(ctx context.Context) []TokenInfo
	UpdateTokens(ctx context.Context, provider string, tokens []string) error
	GetSystemStats(ctx context.Context) (*SystemStats, error)
	GetDownloaderConfig(ctx context.Context) (*DownloaderConfigInfo, error)
	UpdateDownloaderConfig(ctx context.Context, cfg *DownloaderConfigUpdate) error
	GetCookiesContent(ctx context.Context) (string, error)
	UpdateCookiesContent(ctx context.Context, content string) error
	TestDownloader(ctx context.Context, testURL string) (*TestDownloaderResult, error)
}
