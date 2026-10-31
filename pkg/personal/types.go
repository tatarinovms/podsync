// Package personal manages manually curated feeds and shared media objects.
package personal

import (
	"errors"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

var ErrInvalid = errors.New("invalid personal feed request")
var ErrConflict = errors.New("personal feed conflict")

type Feed struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	CoverArt     string        `json:"cover_art"`
	Format       model.Format  `json:"format"`
	PlaylistSort model.Sorting `json:"playlist_sort"`
	PrivateFeed   bool          `json:"private_feed"`
	QuickAddToken string        `json:"quick_add_token,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	FeedURL      string        `json:"feed_url"`
	EpisodeCount int           `json:"episode_count"`
}

type Patch struct {
	Title        *string        `json:"title,omitempty"`
	Description  *string        `json:"description,omitempty"`
	CoverArt     *string        `json:"cover_art,omitempty"`
	Format       *model.Format  `json:"format,omitempty"`
	PlaylistSort *model.Sorting `json:"playlist_sort,omitempty"`
	PrivateFeed  *bool          `json:"private_feed,omitempty"`
}

// Media owns the files. Its format is immutable; copying to a differently
// configured feed preserves the original file and its enclosure MIME type.
type Media struct {
	ID            string              `json:"id"`
	SourceURL     string              `json:"source_url,omitempty"`
	SourcePath    string              `json:"source_path,omitempty"`
	Path          string              `json:"path"`
	Format        model.Format        `json:"format"`
	Quality       model.Quality       `json:"quality,omitempty"`
	MaxHeight     int                 `json:"max_height,omitempty"`
	Title         string              `json:"title"`
	Description   string              `json:"description"`
	Thumbnail     string              `json:"thumbnail"`
	Duration      int64               `json:"duration"`
	Size          int64               `json:"size"`
	Status        model.EpisodeStatus `json:"status"`
	Error         string              `json:"error,omitempty"`
	References    int                 `json:"references"`
	DeletePending bool                `json:"delete_pending,omitempty"`
}

type Membership struct {
	ID      string    `json:"id"`
	MediaID string    `json:"media_id"`
	AddedAt time.Time `json:"added_at"`
}

type Detail struct {
	Feed     *Feed            `json:"feed"`
	Episodes []*model.Episode `json:"episodes"`
}

func (f *Feed) Config() *feed.Config {
	fmt := f.Format
	if fmt == "" || fmt == "universal" {
		fmt = model.FormatCustom
	}
	return &feed.Config{ID: f.ID, URL: "personal://" + f.ID, Format: fmt,
		Quality: model.QualityHigh, PlaylistSort: f.PlaylistSort, PrivateFeed: f.PrivateFeed,
		Custom: feed.Custom{Title: f.Title, Description: f.Description, CoverArt: f.CoverArt}}
}
