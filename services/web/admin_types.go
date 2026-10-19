package web

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

// AdminConfig holds configuration for the admin web panel
type AdminConfig struct {
	Enabled       bool   `toml:"enabled"`
	Username      string `toml:"username"`
	Password      string `toml:"password"`
	ConfigPath    string `toml:"-"`
	MaxAttempts   int    `toml:"max_attempts" json:"max_attempts,omitempty"`
	LockoutPeriod string `toml:"lockout" json:"lockout,omitempty"`
	WindowPeriod  string `toml:"window" json:"window,omitempty"`
}

// FeedSummary contains high-level information about a feed formatted for the admin UI
type FeedStatus string

const (
	FeedStatusIdle     FeedStatus = "idle"
	FeedStatusQueued   FeedStatus = "queued"
	FeedStatusRunning  FeedStatus = "running"
	FeedStatusRetrying FeedStatus = "retrying"
	FeedStatusFailed   FeedStatus = "failed"
)

type FeedStatusItem struct {
	ID          string     `json:"id"`
	Status      FeedStatus `json:"status"`
	LastError   string     `json:"last_error,omitempty"`
	Attempts    int        `json:"attempts,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
}

type FeedSummary struct {
	ID              string       `json:"id"`
	URL             string       `json:"url"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Author          string       `json:"author"`
	CoverArt        string       `json:"cover_art"`
	Provider        string       `json:"provider"`
	Format          string       `json:"format"`
	Quality         string       `json:"quality"`
	PageSize        int          `json:"page_size"`
	CronSchedule    string       `json:"cron_schedule"`
	UpdatePeriod    string       `json:"update_period"`
	EpisodeCount    int          `json:"episode_count"`
	DownloadedCount int          `json:"downloaded_count"`
	ErrorCount      int          `json:"error_count"`
	TotalSize       int64        `json:"total_size"`
	LastUpdate      *time.Time   `json:"last_update,omitempty"`
	Updating        bool         `json:"updating"`
	LastError       string       `json:"last_error,omitempty"`
	LastErrorAt     *time.Time   `json:"last_error_at,omitempty"`
	FeedURL         string       `json:"feed_url"`
	AppleURL        string       `json:"apple_url"`
	PocketCastsURL  string       `json:"pocketcasts_url"`
	OvercastURL     string       `json:"overcast_url"`
	PrivateFeed     bool         `json:"private_feed"`
	Config          *feed.Config `json:"config,omitempty"`
}

// FeedConfigUpdate contains all configurable parameters for an existing feed
type FeedConfigUpdate struct {
	fields map[string]json.RawMessage

	PageSize         int          `json:"page_size"`
	CronSchedule     string       `json:"cron_schedule"`
	Quality          string       `json:"quality"`
	Format           string       `json:"format"`
	MaxHeight        int          `json:"max_height"`
	PlaylistSort     string       `json:"playlist_sort"`
	OPML             *bool        `json:"opml,omitempty"`
	PrivateFeed      bool         `json:"private_feed"`
	KeepLast         int          `json:"keep_last"`
	Language         string       `json:"language,omitempty"`
	Redownload       bool         `json:"redownload,omitempty"`
	Filters          feed.Filters `json:"filters"`
	Custom           feed.Custom  `json:"custom"`
	YouTubeDLArgs    []string     `json:"youtube_dl_args"`
	FilenameTemplate string       `json:"filename_template"`
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
	Language           string `json:"language,omitempty"`
	HasCookiesFile     bool   `json:"has_cookies_file"`
	CookiesFileSize    int64  `json:"cookies_file_size"`
	CookiesFileLines   int    `json:"cookies_file_lines"`
	CookiesLastMod     string `json:"cookies_last_modified"`
	BinaryPath         string `json:"binary_path"`
	BinaryVersion      string `json:"binary_version"`
	FFmpegPath         string `json:"ffmpeg_path,omitempty"`
	FFmpegVersion      string `json:"ffmpeg_version,omitempty"`
}

// DownloaderConfigUpdate contains fields to update downloader settings
type DownloaderConfigUpdate struct {
	Timeout            *int    `json:"timeout"`
	SelfUpdate         *bool   `json:"self_update"`
	CustomBinary       *string `json:"custom_binary"`
	CookiesFile        *string `json:"cookies_file"`
	CookiesFromBrowser *string `json:"cookies_from_browser"`
	Proxy              *string `json:"proxy"`
	Language           *string `json:"language,omitempty"`
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

// DownloadErrorItem represents a failed episode download with diagnostic details
type DownloadErrorItem struct {
	FeedID         string     `json:"feed_id"`
	FeedTitle      string     `json:"feed_title"`
	FeedProvider   string     `json:"feed_provider"`
	EpisodeID      string     `json:"episode_id"`
	EpisodeTitle   string     `json:"episode_title"`
	VideoURL       string     `json:"video_url"`
	PubDate        time.Time  `json:"pub_date"`
	Error          string     `json:"error"`
	ErrorAt        *time.Time `json:"error_at,omitempty"`
	Recommendation string     `json:"recommendation,omitempty"`
}

// RecentDownloadItem represents a recently downloaded episode
type RecentDownloadItem struct {
	FeedID       string    `json:"feed_id"`
	FeedTitle    string    `json:"feed_title"`
	FeedProvider string    `json:"feed_provider"`
	EpisodeID    string    `json:"episode_id"`
	EpisodeTitle string    `json:"episode_title"`
	VideoURL     string    `json:"video_url"`
	PubDate      time.Time `json:"pub_date"`
	Size         int64     `json:"size_bytes"`
	Duration     int64     `json:"duration_seconds"`
	MediaURL     string    `json:"media_url"`
}

// SyncStatusFeedInfo represents per-feed synchronization schedule and status
type SyncStatusFeedInfo struct {
	FeedID          string     `json:"feed_id"`
	FeedTitle       string     `json:"feed_title"`
	URL             string     `json:"url"`
	Provider        string     `json:"provider"`
	CronSchedule    string     `json:"cron_schedule"`
	LastUpdate      *time.Time `json:"last_update,omitempty"`
	NextUpdate      *time.Time `json:"next_update,omitempty"`
	EpisodeCount    int        `json:"episode_count"`
	DownloadedCount int        `json:"downloaded_count"`
	ErrorCount      int        `json:"error_count"`
}

// SyncStatusInfo represents a comprehensive health and sync overview
type SyncStatusInfo struct {
	ActiveFeeds        int                  `json:"active_feeds"`
	TotalEpisodes      int                  `json:"total_episodes"`
	DownloadedEpisodes int                  `json:"downloaded_episodes"`
	FailedEpisodes     int                  `json:"failed_episodes"`
	DownloadedPast24h  int                  `json:"downloaded_past_24h"`
	DiskFreeBytes      uint64               `json:"disk_free_bytes"`
	DiskTotalBytes     uint64               `json:"disk_total_bytes"`
	HasDownloader      bool                 `json:"has_downloader"`
	HasFFmpeg          bool                 `json:"has_ffmpeg"`
	Feeds              []SyncStatusFeedInfo `json:"feeds"`
}

// LogEntry represents a single structured log line.
type LogEntry struct {
	ID        int64                  `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
}

// LogFilter specifies criteria for querying logs.
type LogFilter struct {
	Level   string `json:"level"`
	Search  string `json:"search"`
	FeedID  string `json:"feed_id"`
	Limit   int    `json:"limit"`
	SinceID int64  `json:"since_id"`
}

// LogResult represents the queried logs response.
type LogResult struct {
	Entries       []LogEntry `json:"entries"`
	Total         int        `json:"total"`
	FilteredTotal int        `json:"filtered_total"`
	MaxID         int64      `json:"max_id"`
	CurrentLevel  string     `json:"current_level"`
}

// AudiobookDirInfo holds metadata about a discovered local audiobook directory
type AudiobookDirInfo struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	FileCount       int    `json:"file_count"`
	HasCover        bool   `json:"has_cover"`
	TotalSize       int64  `json:"total_size"`
	Linked          bool   `json:"linked"`
	FeedID          string `json:"feed_id,omitempty"`
	SuggestedTitle  string `json:"suggested_title"`
	SuggestedAuthor string `json:"suggested_author"`
}

// ImportAudiobookRequest specifies the parameters for creating an audiobook feed
type ImportAudiobookRequest struct {
	DirName      string `json:"dir_name"`
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	Author       string `json:"author,omitempty"`
	Description  string `json:"description,omitempty"`
	CronSchedule string `json:"cron_schedule,omitempty"`
	PrivateFeed  bool   `json:"private_feed,omitempty"`
}

// AdminManager is the interface required by the web admin handlers
type AdminManager interface {
	ListFeeds(ctx context.Context) ([]FeedSummary, error)
	GetFeedsStatus(ctx context.Context) ([]FeedStatusItem, error)
	GetFeedDetail(ctx context.Context, id string) (*FeedSummary, []*model.Episode, error)
	AddFeed(ctx context.Context, cfg *feed.Config) error
	UpdateFeed(ctx context.Context, id string, update *FeedConfigUpdate) error
	DeleteFeed(ctx context.Context, id string, deleteFiles bool) error
	TriggerUpdate(ctx context.Context, id string) error
	TriggerUpdateAll(ctx context.Context) ([]string, error)
	RetryEpisode(ctx context.Context, feedID, episodeID string) error
	GetTokens(ctx context.Context) []TokenInfo
	UpdateTokens(ctx context.Context, provider string, tokens []string) error
	AddTokens(ctx context.Context, provider string, tokens []string) error
	DeleteToken(ctx context.Context, provider string, index int) error
	GetSystemStats(ctx context.Context) (*SystemStats, error)
	GetDownloaderConfig(ctx context.Context) (*DownloaderConfigInfo, error)
	UpdateDownloaderConfig(ctx context.Context, cfg *DownloaderConfigUpdate) error
	GetCookiesContent(ctx context.Context) (string, error)
	UpdateCookiesContent(ctx context.Context, content string) error
	TestDownloader(ctx context.Context, testURL string) (*TestDownloaderResult, error)
	GetDownloadErrors(ctx context.Context, feedID string, limit int) ([]DownloadErrorItem, error)
	GetRecentDownloads(ctx context.Context, feedID string, limit int) ([]RecentDownloadItem, error)
	GetSyncStatus(ctx context.Context) (*SyncStatusInfo, error)
	ChangePassword(ctx context.Context, oldPassword, newPassword string) error

	// Logs management
	GetSystemLogs(ctx context.Context, filter LogFilter) (*LogResult, error)
	ClearSystemLogs(ctx context.Context) error
	SetLogLevel(ctx context.Context, level string) (string, error)
	GetLogLevel(ctx context.Context) string
	DownloadLogs(ctx context.Context) ([]byte, string, error)

	// Audiobook management
	ListAudiobookDirs(ctx context.Context) ([]AudiobookDirInfo, error)
	ImportAudiobook(ctx context.Context, req *ImportAudiobookRequest) (*feed.Config, error)
	GetAudiobooksDir() string
	SaveAudiobookFile(bookDir string, filename string, r io.Reader) error
	DeleteAudiobookDir(ctx context.Context, dirName string) error
}
