package update

import (
	"bytes"
	"context"
	"io"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
)

type liveSkippingDownloader struct{}

func (liveSkippingDownloader) Download(context.Context, *feed.Config, *model.Episode) (io.ReadCloser, error) {
	return nil, ytdl.ErrLiveStreamSkipped
}
func (liveSkippingDownloader) PlaylistMetadata(context.Context, string) (ytdl.PlaylistMetadata, error) {
	return ytdl.PlaylistMetadata{}, nil
}

func TestUpdater_Cleanup_RetainsNewestByDate(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dataDir := t.TempDir()

	database, err := db.NewBadger(&db.Config{Dir: dbDir})
	require.NoError(t, err)
	defer database.Close()

	storage, err := fs.NewLocal(dataDir, false, false)
	require.NoError(t, err)

	feedID := "test_feed"
	feedConfig := &feed.Config{
		ID:     feedID,
		Format: model.FormatAudio,
		Clean: &feed.Cleanup{
			KeepLast: 2,
		},
	}

	// Episode 1: ID alphabetically first ("aaa_oldest"), but published furthest in past
	// Episode 2: ID alphabetically middle ("mmm_middle")
	// Episode 3: ID alphabetically last ("zzz_newest"), but published most recently
	// In BadgerDB key order: aaa_oldest -> mmm_middle -> zzz_newest
	now := time.Now()
	episodes := []*model.Episode{
		{
			ID:       "aaa_oldest",
			Title:    "Oldest Episode",
			PubDate:  now.Add(-48 * time.Hour),
			Status:   model.EpisodeDownloaded,
			VideoURL: "http://example.com/aaa",
		},
		{
			ID:       "mmm_middle",
			Title:    "Middle Episode",
			PubDate:  now.Add(-24 * time.Hour),
			Status:   model.EpisodeDownloaded,
			VideoURL: "http://example.com/mmm",
		},
		{
			ID:       "zzz_newest",
			Title:    "Newest Episode",
			PubDate:  now,
			Status:   model.EpisodeDownloaded,
			VideoURL: "http://example.com/zzz",
		},
	}

	// Create test feed with episodes in database
	err = database.AddFeed(ctx, feedID, &model.Feed{ID: feedID, Episodes: episodes})
	require.NoError(t, err)

	for _, ep := range episodes {
		// Create file on storage
		filename := fmt.Sprintf("%s/%s", feedID, feed.EpisodeName(feedConfig, ep))
		_, createErr := storage.Create(ctx, filename, bytes.NewBufferString("sample audio content"))
		require.NoError(t, createErr)
	}

	mgr, err := NewUpdater(
		map[string]*feed.Config{feedID: feedConfig},
		nil,
		"http://localhost:8080",
		nil,
		database,
		storage,
	)
	require.NoError(t, err)

	// Run cleanup with KeepLast = 2
	err = mgr.cleanup(ctx, feedConfig)
	require.NoError(t, err)

	// Verify zzz_newest is still downloaded
	epNewest, err := database.GetEpisode(ctx, feedID, "zzz_newest")
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeDownloaded, epNewest.Status, "Newest episode must not be cleaned!")

	// Verify mmm_middle is still downloaded
	epMiddle, err := database.GetEpisode(ctx, feedID, "mmm_middle")
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeDownloaded, epMiddle.Status, "Middle episode must not be cleaned!")

	// Verify aaa_oldest is purged/cleaned
	epOldest, err := database.GetEpisode(ctx, feedID, "aaa_oldest")
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeCleaned, epOldest.Status, "Oldest episode must be cleaned!")
	assert.Equal(t, int64(0), epOldest.Size)

	// Verify old file was deleted from disk
	oldestFile := fmt.Sprintf("%s/%s", feedID, feed.EpisodeName(feedConfig, episodes[0]))
	_, statErr := storage.Size(ctx, oldestFile)
	assert.Error(t, statErr, "Oldest file should have been removed from disk")

	// Verify newest file remains on disk
	newestFile := fmt.Sprintf("%s/%s", feedID, feed.EpisodeName(feedConfig, episodes[2]))
	size, statErr := storage.Size(ctx, newestFile)
	assert.NoError(t, statErr)
	assert.Greater(t, size, int64(0))
}

func TestUpdater_FetchEpisodes_SortsByPubDate(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()

	database, err := db.NewBadger(&db.Config{Dir: dbDir})
	require.NoError(t, err)
	defer database.Close()

	feedID := "fetch_feed"
	feedConfig := &feed.Config{
		ID:       feedID,
		Format:   model.FormatAudio,
		PageSize: 2, // Only fetch top 2
	}

	now := time.Now()
	// Add 3 new episodes
	eps := []*model.Episode{
		{ID: "ep_old", Title: "Old", Status: model.EpisodeNew, PubDate: now.Add(-10 * time.Hour)},
		{ID: "ep_new", Title: "New", Status: model.EpisodeNew, PubDate: now},
		{ID: "ep_mid", Title: "Mid", Status: model.EpisodeNew, PubDate: now.Add(-5 * time.Hour)},
	}

	err = database.AddFeed(ctx, feedID, &model.Feed{ID: feedID, Episodes: eps})
	require.NoError(t, err)

	mgr, err := NewUpdater(
		map[string]*feed.Config{feedID: feedConfig},
		nil,
		"http://localhost:8080",
		nil,
		database,
		nil,
	)
	require.NoError(t, err)

	fetched, err := mgr.fetchEpisodes(ctx, feedConfig)
	require.NoError(t, err)
	require.Len(t, fetched, 2)
	assert.Equal(t, "ep_new", fetched[0].ID, "Newest episode must come first")
	assert.Equal(t, "ep_mid", fetched[1].ID, "Second newest episode must come second")
}

func TestUpdaterPublishesPodcastingChapters(t *testing.T) {
	ctx := context.Background()
	storage, err := fs.NewLocal(t.TempDir(), false, false)
	require.NoError(t, err)
	mgr, err := NewUpdater(nil, nil, "https://podsync.example", nil, nil, storage)
	require.NoError(t, err)
	episode := &model.Episode{ID: "../../provider-id", Description: "00:00 Intro\n01:10 Main topic"}
	require.NoError(t, mgr.writeChapterDocuments(ctx, "feed", []*model.Episode{episode}))
	require.Len(t, episode.Chapters, 2)
	require.Equal(t, "https://podsync.example/"+chapterObjectName("feed", episode.ID), episode.ChaptersURL)
	chapterFile, err := storage.Open(chapterObjectName("feed", episode.ID))
	require.NoError(t, err)
	defer chapterFile.Close()
	contents, err := io.ReadAll(chapterFile)
	require.NoError(t, err)
	assert.Contains(t, string(contents), `"version": "1.2.0"`)
}

func TestUpdaterDoesNotMarkSkippedLiveEpisodeAsError(t *testing.T) {
	ctx := context.Background()
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	defer database.Close()
	storage, err := fs.NewLocal(t.TempDir(), false, false)
	require.NoError(t, err)
	config := &feed.Config{ID: "feed", Format: model.FormatAudio, IgnoreLive: true}
	episode := &model.Episode{ID: "live", Title: "Live", Status: model.EpisodeNew}
	require.NoError(t, database.AddFeed(ctx, config.ID, &model.Feed{ID: config.ID, Episodes: []*model.Episode{episode}}))
	mgr, err := NewUpdater(map[string]*feed.Config{config.ID: config}, nil, "https://podsync.example", liveSkippingDownloader{}, database, storage)
	require.NoError(t, err)
	require.NoError(t, mgr.downloadEpisodes(ctx, config, []*model.Episode{episode}))
	updated, err := database.GetEpisode(ctx, config.ID, episode.ID)
	require.NoError(t, err)
	assert.Equal(t, model.EpisodeCleaned, updated.Status)
	assert.Empty(t, updated.Error)
}
