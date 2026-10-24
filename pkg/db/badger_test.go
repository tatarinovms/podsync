package db

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/model"
)

var testCtx = context.TODO()

func TestFeedStatsConcurrentEpisodeUpdates(t *testing.T) {
	database, err := NewBadger(&Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	f := getFeed()
	require.NoError(t, database.AddFeed(testCtx, f.ID, f))
	var started sync.WaitGroup
	started.Add(2)
	results := make(chan error, 2)
	for _, id := range []string{"1", "2"} {
		go func(id string) {
			firstAttempt := true
			results <- database.UpdateEpisode(f.ID, id, func(ep *model.Episode) error {
				if firstAttempt {
					firstAttempt = false
					started.Done()
					started.Wait()
				}
				ep.Status = model.EpisodeDownloaded
				ep.Size = 100
				return nil
			})
		}(id)
	}
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	stats, err := database.GetFeedStats(testCtx, f.ID)
	require.NoError(t, err)
	assert.Equal(t, &model.FeedStats{EpisodeCount: 2, DownloadedCount: 2, TotalSize: 200}, stats)
}

func TestFeedStatsLegacyAndAudiobookSnapshots(t *testing.T) {
	database, err := NewBadger(&Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	f := &model.Feed{ID: "book", Provider: model.ProviderAudiobook, Episodes: []*model.Episode{
		{ID: "1", Status: model.EpisodeDownloaded, Size: 100},
		{ID: "2", Status: model.EpisodeError},
	}}
	require.NoError(t, database.AddFeed(testCtx, f.ID, f))
	// Simulate a database created before stats were introduced.
	require.NoError(t, database.db.Update(func(txn *badger.Txn) error { return txn.Delete(database.getKey(statsPath, f.ID)) }))
	require.NoError(t, database.UpdateEpisode(f.ID, "2", func(ep *model.Episode) error { ep.Status = model.EpisodeDownloaded; ep.Size = 200; return nil }))
	stats, err := database.GetFeedStats(testCtx, f.ID)
	require.NoError(t, err)
	assert.Equal(t, &model.FeedStats{EpisodeCount: 2, DownloadedCount: 2, TotalSize: 300}, stats)
	// Removing a chapter and replacing another must update the same aggregate.
	f.Episodes = []*model.Episode{{ID: "2", Status: model.EpisodeDownloaded, Size: 250}, {ID: "3", Status: model.EpisodeNew}}
	require.NoError(t, database.AddFeed(testCtx, f.ID, f))
	stats, err = database.GetFeedStats(testCtx, f.ID)
	require.NoError(t, err)
	assert.Equal(t, &model.FeedStats{EpisodeCount: 2, DownloadedCount: 1, TotalSize: 250}, stats)
	require.NoError(t, database.DeleteEpisode(f.ID, "2"))
	require.NoError(t, database.DeleteEpisode(f.ID, "2"))
	stats, err = database.GetFeedStats(testCtx, f.ID)
	require.NoError(t, err)
	assert.Equal(t, &model.FeedStats{EpisodeCount: 1}, stats)
}

func TestActiveJobsIndexMigrationBatchesAndTransitions(t *testing.T) {
	raw, err := badger.Open(badger.DefaultOptions(t.TempDir()).WithMaxTableSize(1 << 20).WithLogger(nil))
	require.NoError(t, err)
	database := &Badger{db: raw}
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	batch := raw.NewWriteBatch()
	defer batch.Cancel()
	for i := 0; i < 300; i++ {
		j := &model.UpdateJob{ID: fmt.Sprintf("update/%03d", i), FeedID: fmt.Sprint(i), Status: model.JobStatusQueued, LastError: strings.Repeat("x", 2048)}
		data, err := database.marshalObj(j)
		require.NoError(t, err)
		require.NoError(t, batch.Set(database.getKey(jobPath, j.ID), data))
	}
	require.NoError(t, batch.Flush())
	require.NoError(t, database.initActiveJobsIndex())
	count := func() int {
		n := 0
		require.NoError(t, database.WalkActiveJobs(testCtx, func(*model.UpdateJob) error { n++; return nil }))
		return n
	}
	assert.Equal(t, 300, count())
	j, err := database.GetJob(testCtx, "update/000")
	require.NoError(t, err)
	j.Status = model.JobStatusDone
	require.NoError(t, database.SaveJob(testCtx, j))
	require.NoError(t, database.DeleteJob(testCtx, "update/001"))
	require.NoError(t, database.initActiveJobsIndex())
	assert.Equal(t, 298, count())
}

func TestActiveJobsIndexMigrationReportsCorruption(t *testing.T) {
	dir := t.TempDir()
	database, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	require.NoError(t, database.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(database.getKey(activeJobIndexVersionPath)); err != nil {
			return err
		}
		return txn.Set(database.getKey(jobPath, "broken"), []byte("invalid JSON"))
	}))
	require.NoError(t, database.Close())
	_, err = NewBadger(&Config{Dir: dir})
	require.ErrorContains(t, err, "active jobs index")
}

func TestNewBadger(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)

	err = db.Close()
	assert.NoError(t, err)
}

func TestBadger_Version(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	ver, err := db.Version()
	assert.NoError(t, err)
	assert.Equal(t, CurrentVersion, ver)
}

func TestBadger_AddFeed(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	err = db.AddFeed(testCtx, feed.ID, feed)
	assert.NoError(t, err)
}

func TestBadger_GetFeed(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	feed.Episodes = nil

	err = db.AddFeed(testCtx, feed.ID, feed)
	require.NoError(t, err)

	actual, err := db.GetFeed(testCtx, feed.ID)
	assert.NoError(t, err)
	assert.Equal(t, feed, actual)
}

func TestBadger_WalkFeeds(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	feed.Episodes = nil // These are not serialized to database

	err = db.AddFeed(testCtx, feed.ID, feed)
	assert.NoError(t, err)

	called := 0
	err = db.WalkFeeds(testCtx, func(actual *model.Feed) error {
		assert.EqualValues(t, feed, actual)
		called++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, called, 1)
}

func TestBadger_DeleteFeed(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	err = db.AddFeed(testCtx, feed.ID, feed)
	require.NoError(t, err)

	err = db.DeleteFeed(testCtx, feed.ID)
	assert.NoError(t, err)

	called := 0
	err = db.WalkFeeds(testCtx, func(feed *model.Feed) error {
		called++
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, called)
}

func TestBadger_UpdateEpisode(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	err = db.AddFeed(testCtx, feed.ID, feed)
	assert.NoError(t, err)

	err = db.UpdateEpisode(feed.ID, feed.Episodes[0].ID, func(file *model.Episode) error {
		file.Size = 333
		file.Status = model.EpisodeDownloaded
		return nil
	})
	assert.NoError(t, err)

	episode, err := db.GetEpisode(testCtx, feed.ID, feed.Episodes[0].ID)
	assert.NoError(t, err)

	assert.Equal(t, feed.Episodes[0].ID, episode.ID)
	assert.EqualValues(t, 333, episode.Size)
	assert.Equal(t, model.EpisodeDownloaded, episode.Status)

	assert.NoError(t, err)
}

func TestBadger_WalkEpisodes(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	err = db.AddFeed(testCtx, feed.ID, feed)
	assert.NoError(t, err)

	called := 0
	err = db.WalkEpisodes(testCtx, feed.ID, func(actual *model.Episode) error {
		assert.EqualValues(t, feed.Episodes[called], actual)
		called++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, called, 2)
}

func getFeed() *model.Feed {
	return &model.Feed{
		ID:             "1",
		ItemID:         "2",
		LinkType:       model.TypeChannel,
		Provider:       model.ProviderVimeo,
		CreatedAt:      time.Now().UTC(),
		LastAccess:     time.Now().UTC(),
		ExpirationTime: time.Now().UTC().Add(1 * time.Hour),
		Format:         "video",
		Quality:        "high",
		PageSize:       50,
		Title:          "Test",
		Description:    "Test",
		PubDate:        time.Now().UTC(),
		Author:         "",
		ItemURL:        "https://vimeo.com",
		Episodes: []*model.Episode{
			{
				ID:          "1",
				Title:       "Episode title 1",
				Description: "Episode description 1",
				Duration:    100,
				VideoURL:    "https://vimeo.com/123",
				PubDate:     time.Now().UTC(),
				Size:        1234,
				Order:       "1",
			},
			{
				ID:          "2",
				Title:       "Episode title 2",
				Description: "Episode description 2",
				Duration:    299,
				VideoURL:    "https://vimeo.com/321",
				PubDate:     time.Now().UTC(),
				Size:        4321,
				Order:       "2",
			},
		},
		UpdatedAt: time.Now().UTC(),
	}
}

func TestAudiobookSnapshotRefreshesAndRemovesChapters(t *testing.T) {
	database, err := NewBadger(&Config{Dir: t.TempDir()})
	require.NoError(t, err)
	defer database.Close()
	ctx := context.Background()
	book := &model.Feed{ID: "book", Provider: model.ProviderAudiobook, Episodes: []*model.Episode{
		{ID: "01.mp3", Title: "Old title", Status: model.EpisodeDownloaded},
		{ID: "02.mp3", Title: "Removed", Status: model.EpisodeDownloaded},
	}}
	require.NoError(t, database.AddFeed(ctx, book.ID, book))
	book.Episodes = []*model.Episode{{ID: "01.mp3", Title: "New title", Size: 42, Order: "1", Status: model.EpisodeDownloaded}}
	require.NoError(t, database.AddFeed(ctx, book.ID, book))
	got, err := database.GetFeed(ctx, book.ID)
	require.NoError(t, err)
	require.Len(t, got.Episodes, 1)
	assert.Equal(t, "New title", got.Episodes[0].Title)
	assert.Equal(t, int64(42), got.Episodes[0].Size)
	book.Episodes = []*model.Episode{}
	require.NoError(t, database.AddFeed(ctx, book.ID, book))
	got, err = database.GetFeed(ctx, book.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Episodes)
}

func TestBadger_Jobs(t *testing.T) {
	dir := t.TempDir()
	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// 1. Not found
	_, err = db.GetJob(ctx, "update/test")
	assert.Equal(t, model.ErrNotFound, err)

	now := time.Now().UTC().Truncate(time.Second)
	job := &model.UpdateJob{
		ID:          "update/test",
		FeedID:      "test",
		Kind:        model.JobKindUpdate,
		Status:      model.JobStatusQueued,
		Attempts:    0,
		MaxAttempts: 5,
		AvailableAt: now,
		CreatedAt:   now,
	}

	// 2. SaveJob
	err = db.SaveJob(ctx, job)
	require.NoError(t, err)

	// 3. GetJob
	saved, err := db.GetJob(ctx, "update/test")
	require.NoError(t, err)
	assert.Equal(t, job.ID, saved.ID)
	assert.Equal(t, model.JobStatusQueued, saved.Status)

	// 4. Update status to running
	saved.Status = model.JobStatusRunning
	started := now.Add(time.Second)
	saved.StartedAt = &started
	err = db.SaveJob(ctx, saved)
	require.NoError(t, err)

	updated, err := db.GetJob(ctx, "update/test")
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusRunning, updated.Status)
	assert.NotNil(t, updated.StartedAt)

	// 5. WalkJobs
	job2 := &model.UpdateJob{
		ID:          "retry/test/ep1",
		FeedID:      "test",
		EpisodeID:   "ep1",
		Kind:        model.JobKindRetry,
		Status:      model.JobStatusRetrying,
		Attempts:    1,
		MaxAttempts: 5,
		AvailableAt: now.Add(time.Minute),
		CreatedAt:   now,
	}
	require.NoError(t, db.SaveJob(ctx, job2))

	var list []*model.UpdateJob
	err = db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		list = append(list, j)
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// 6. DeleteJob
	err = db.DeleteJob(ctx, "update/test")
	require.NoError(t, err)
	_, err = db.GetJob(ctx, "update/test")
	assert.Equal(t, model.ErrNotFound, err)
}

func TestBadger_GetFeedMetadata(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	err = db.AddFeed(testCtx, feed.ID, feed)
	require.NoError(t, err)

	meta, err := db.GetFeedMetadata(testCtx, feed.ID)
	require.NoError(t, err)
	assert.Equal(t, feed.ID, meta.ID)
	assert.Equal(t, feed.Title, meta.Title)
	assert.Equal(t, feed.Author, meta.Author)
	assert.Nil(t, meta.Episodes, "GetFeedMetadata must not load episodes")
}

func TestBadger_GetFeedStats(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBadger(&Config{Dir: dir})
	require.NoError(t, err)
	defer db.Close()

	feed := getFeed()
	err = db.AddFeed(testCtx, feed.ID, feed)
	require.NoError(t, err)

	stats, err := db.GetFeedStats(testCtx, feed.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.EpisodeCount)
	assert.Equal(t, 0, stats.DownloadedCount)
	assert.Equal(t, 0, stats.ErrorCount)
	assert.Equal(t, int64(0), stats.TotalSize)

	// Update episode 1 to downloaded
	err = db.UpdateEpisode(feed.ID, "1", func(ep *model.Episode) error {
		ep.Status = model.EpisodeDownloaded
		ep.Size = 1000
		return nil
	})
	require.NoError(t, err)

	stats, err = db.GetFeedStats(testCtx, feed.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.EpisodeCount)
	assert.Equal(t, 1, stats.DownloadedCount)
	assert.Equal(t, int64(1000), stats.TotalSize)

	// Delete episode 2
	err = db.DeleteEpisode(feed.ID, "2")
	require.NoError(t, err)

	stats, err = db.GetFeedStats(testCtx, feed.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.EpisodeCount)
	assert.Equal(t, 1, stats.DownloadedCount)
	assert.Equal(t, int64(1000), stats.TotalSize)
}

func BenchmarkWalkEpisodes(b *testing.B) {
	dir := b.TempDir()
	database, err := NewBadger(&Config{Dir: dir})
	require.NoError(b, err)
	defer database.Close()

	ctx := context.Background()
	feed := &model.Feed{
		ID:    "bench-feed",
		Title: "Bench",
	}
	for i := 0; i < 500; i++ {
		feed.Episodes = append(feed.Episodes, &model.Episode{
			ID:       fmt.Sprintf("ep-%d", i),
			Title:    fmt.Sprintf("Episode %d", i),
			Status:   model.EpisodeDownloaded,
			Duration: 1800,
			Size:     1024 * 1024 * 10,
		})
	}
	require.NoError(b, database.AddFeed(ctx, feed.ID, feed))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count := 0
		_ = database.WalkEpisodes(ctx, feed.ID, func(ep *model.Episode) error {
			count++
			return nil
		})
	}
}

func BenchmarkGetFeedMetadata(b *testing.B) {
	dir := b.TempDir()
	database, err := NewBadger(&Config{Dir: dir})
	require.NoError(b, err)
	defer database.Close()

	ctx := context.Background()
	feed := &model.Feed{
		ID:    "bench-feed",
		Title: "Bench",
	}
	for i := 0; i < 500; i++ {
		feed.Episodes = append(feed.Episodes, &model.Episode{
			ID:       fmt.Sprintf("ep-%d", i),
			Title:    fmt.Sprintf("Episode %d", i),
			Status:   model.EpisodeDownloaded,
			Duration: 1800,
			Size:     1024 * 1024 * 10,
		})
	}
	require.NoError(b, database.AddFeed(ctx, feed.ID, feed))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = database.GetFeedMetadata(ctx, feed.ID)
	}
}

func BenchmarkGetFeedStats(b *testing.B) {
	dir := b.TempDir()
	database, err := NewBadger(&Config{Dir: dir})
	require.NoError(b, err)
	defer database.Close()

	ctx := context.Background()
	feed := &model.Feed{
		ID:    "bench-feed",
		Title: "Bench",
	}
	for i := 0; i < 500; i++ {
		feed.Episodes = append(feed.Episodes, &model.Episode{
			ID:       fmt.Sprintf("ep-%d", i),
			Title:    fmt.Sprintf("Episode %d", i),
			Status:   model.EpisodeDownloaded,
			Duration: 1800,
			Size:     1024 * 1024 * 10,
		})
	}
	require.NoError(b, database.AddFeed(ctx, feed.ID, feed))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = database.GetFeedStats(ctx, feed.ID)
	}
}
