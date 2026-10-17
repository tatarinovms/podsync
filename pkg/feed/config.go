package feed

import (
	"time"

	"github.com/mxpv/podsync/pkg/model"
)

// Config is a configuration for a feed loaded from TOML
type Config struct {
	ID string `toml:"-" json:"id,omitempty"`
	// URL is a full URL of the field
	URL string `toml:"url" json:"url"`
	// PageSize is the number of pages to query from YouTube API.
	// NOTE: larger page sizes/often requests might drain your API token.
	PageSize int `toml:"page_size" json:"page_size"`
	// UpdatePeriod is how often to check for updates.
	// Format is "300ms", "1.5h" or "2h45m".
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	// NOTE: too often update check might drain your API token.
	UpdatePeriod time.Duration `toml:"update_period" json:"update_period,omitempty"`
	// Cron expression format is how often to check update
	// NOTE: too often update check might drain your API token.
	CronSchedule string `toml:"cron_schedule" json:"cron_schedule,omitempty"`
	// Quality to use for this feed
	Quality model.Quality `toml:"quality" json:"quality"`
	// Maximum height of video
	MaxHeight int `toml:"max_height" json:"max_height,omitempty"`
	// Format to use for this feed
	Format model.Format `toml:"format" json:"format"`
	// Custom format properties
	CustomFormat CustomFormat `toml:"custom_format" json:"custom_format,omitempty"`
	// Only download episodes that match the filters (defaults to matching anything)
	Filters Filters `toml:"filters" json:"filters,omitempty"`
	// Clean is a cleanup policy to use for this feed
	Clean *Cleanup `toml:"clean" json:"clean,omitempty"`
	// Custom is a list of feed customizations
	Custom Custom `toml:"custom" json:"custom,omitempty"`
	// List of additional youtube-dl arguments passed at download time
	YouTubeDLArgs []string `toml:"youtube_dl_args" json:"youtube_dl_args,omitempty"`
	// Post episode download hooks - executed after each episode is successfully downloaded
	// Multiple hooks can be configured and will execute in sequence
	// Example:
	//   [[feeds.ID1.post_episode_download]]
	//   command = ["echo", "Downloaded: $EPISODE_TITLE"]
	//   timeout = 10
	PostEpisodeDownload []*ExecHook `toml:"post_episode_download" json:"post_episode_download,omitempty"`
	// Episode download error hooks - executed when an episode download fails
	// Available environment variables: FEED_NAME, EPISODE_TITLE, ERROR_MESSAGE
	// Multiple hooks can be configured and will execute in sequence
	// Example:
	//   [[feeds.ID1.on_episode_download_error]]
	//   command = ["curl", "-X", "POST", "-d", "Download failed: $ERROR_MESSAGE", "https://webhook.example.com/notify"]
	//   timeout = 30
	OnEpisodeDownloadError []*ExecHook `toml:"on_episode_download_error" json:"on_episode_download_error,omitempty"`
	// Included in OPML file (defaults to true if omitted)
	OPML *bool `toml:"opml" json:"opml,omitempty"`
	// Private feed (not indexed by podcast aggregators)
	PrivateFeed bool `toml:"private_feed" json:"private_feed,omitempty"`
	// Playlist sort
	PlaylistSort model.Sorting `toml:"playlist_sort" json:"playlist_sort,omitempty"`
	// FilenameTemplate controls output media filename (without extension)
	// Supported tokens: {{id}}, {{title}}, {{pub_date}}, {{feed_id}}
	FilenameTemplate string `toml:"filename_template" json:"filename_template,omitempty"`
}

type CustomFormat struct {
	YouTubeDLFormat string `toml:"youtube_dl_format" json:"youtube_dl_format,omitempty"`
	Extension       string `toml:"extension" json:"extension,omitempty"`
}

type Filters struct {
	Title          string `toml:"title" json:"title,omitempty"`
	NotTitle       string `toml:"not_title" json:"not_title,omitempty"`
	Description    string `toml:"description" json:"description,omitempty"`
	NotDescription string `toml:"not_description" json:"not_description,omitempty"`
	MinDuration    int64  `toml:"min_duration" json:"min_duration,omitempty"`
	MaxDuration    int64  `toml:"max_duration" json:"max_duration,omitempty"`
	MaxAge         int    `toml:"max_age" json:"max_age,omitempty"`
	MinAge         int    `toml:"min_age" json:"min_age,omitempty"`
	// More filters to be added here
}

type Custom struct {
	CoverArt        string        `toml:"cover_art" json:"cover_art,omitempty"`
	CoverArtQuality model.Quality `toml:"cover_art_quality" json:"cover_art_quality,omitempty"`
	Category        string        `toml:"category" json:"category,omitempty"`
	Subcategories   []string      `toml:"subcategories" json:"subcategories,omitempty"`
	Explicit        bool          `toml:"explicit" json:"explicit,omitempty"`
	Language        string        `toml:"lang" json:"lang,omitempty"`
	Author          string        `toml:"author" json:"author,omitempty"`
	Title           string        `toml:"title" json:"title,omitempty"`
	Description     string        `toml:"description" json:"description,omitempty"`
	OwnerName       string        `toml:"ownerName" json:"owner_name,omitempty"`
	OwnerEmail      string        `toml:"ownerEmail" json:"owner_email,omitempty"`
	Link            string        `toml:"link" json:"link,omitempty"`
}

type Cleanup struct {
	// KeepLast defines how many episodes to keep
	KeepLast int `toml:"keep_last" json:"keep_last"`
}
