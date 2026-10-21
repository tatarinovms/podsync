package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/jobqueue"
	"github.com/mxpv/podsync/pkg/ytdl"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
)

func TestSaveAudiobookFileRejectsSymlinkAndExistingFile(t *testing.T) {
	root := t.TempDir()
	books := filepath.Join(root, "books")
	outside := filepath.Join(root, "outside")
	require.NoError(t, os.MkdirAll(books, 0755))
	require.NoError(t, os.MkdirAll(outside, 0755))
	require.NoError(t, os.Symlink(outside, filepath.Join(books, "linked")))
	mgr := &AppFeedManager{cfg: &Config{Audiobooks: AudiobooksConfig{Dir: books}}}
	require.Error(t, mgr.SaveAudiobookFile("linked", "chapter.mp3", strings.NewReader("audio")))
	_, err := os.Stat(filepath.Join(outside, "chapter.mp3"))
	assert.True(t, os.IsNotExist(err))
	require.NoError(t, mgr.SaveAudiobookFile("book", "chapter.mp3", strings.NewReader("first")))
	require.Error(t, mgr.SaveAudiobookFile("book", "chapter.mp3", strings.NewReader("second")))
	content, err := os.ReadFile(filepath.Join(books, "book", "chapter.mp3"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(content))
}

func TestSyncCookiesToFeeds(t *testing.T) {
	mgr := &AppFeedManager{
		cfg: &Config{
			Feeds: map[string]*feed.Config{
				"podcast1": {
					ID:            "podcast1",
					YouTubeDLArgs: []string{"--js-runtimes", "deno:/root/.deno/bin/deno", "--cookies", "/etc/podsync/youtube-cookies.txt"},
				},
				"podcast2": {
					ID:            "podcast2",
					YouTubeDLArgs: []string{"--cookies=/old/path/cookies.txt", "--geo-bypass"},
				},
				"podcast3": {
					ID:            "podcast3",
					YouTubeDLArgs: []string{"--geo-bypass"},
				},
			},
		},
	}

	newCookiesPath := "/mnt/dietpi_userdata/podsync/data/cookies.txt"
	mgr.syncCookiesToFeeds(newCookiesPath)

	assert.Equal(t, []string{"--js-runtimes", "deno:/root/.deno/bin/deno", "--cookies", newCookiesPath}, mgr.cfg.Feeds["podcast1"].YouTubeDLArgs)
	assert.Equal(t, []string{"--cookies=" + newCookiesPath, "--geo-bypass"}, mgr.cfg.Feeds["podcast2"].YouTubeDLArgs)
	assert.Equal(t, []string{"--geo-bypass"}, mgr.cfg.Feeds["podcast3"].YouTubeDLArgs)

	// When cookies are cleared
	mgr.syncCookiesToFeeds("")
	assert.Equal(t, []string{"--js-runtimes", "deno:/root/.deno/bin/deno"}, mgr.cfg.Feeds["podcast1"].YouTubeDLArgs)
	assert.Equal(t, []string{"--geo-bypass"}, mgr.cfg.Feeds["podcast2"].YouTubeDLArgs)
}

func TestTriggerUpdateAll(t *testing.T) {
	updates := make(chan *feed.Config, 10)
	mgr := &AppFeedManager{
		cfg: &Config{
			Feeds: map[string]*feed.Config{
				"feed1": {ID: "feed1"},
				"feed2": {ID: "feed2"},
			},
		},
		feeds: map[string]*feed.Config{
			"feed1": {ID: "feed1"},
			"feed2": {ID: "feed2"},
		},
		updates: updates,
	}

	queued, err := mgr.TriggerUpdateAll(context.Background())
	assert.NoError(t, err)
	assert.Len(t, queued, 2)
	assert.Contains(t, queued, "feed1")
	assert.Contains(t, queued, "feed2")

	// Verify channel received both
	assert.Len(t, updates, 2)

	// If channel is full
	fullChan := make(chan *feed.Config, 1)
	mgr.updates = fullChan
	fullChan <- &feed.Config{ID: "blocking"}

	queuedFull, err := mgr.TriggerUpdateAll(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, queuedFull)
}

func TestFeedUpdateStatusFollowsQueueAndRun(t *testing.T) {
	updates := make(chan *feed.Config, 2)
	mgr := &AppFeedManager{
		feeds:   map[string]*feed.Config{"feed1": {ID: "feed1"}},
		updates: updates,
	}
	require.NoError(t, mgr.TriggerUpdate(context.Background(), "feed1"))
	assert.Equal(t, 1, mgr.queuedUpdates["feed1"])
	mgr.MarkFeedUpdateStarted("feed1")
	assert.Zero(t, mgr.queuedUpdates["feed1"])
	assert.True(t, mgr.runningUpdates["feed1"])
	require.NoError(t, mgr.TriggerUpdate(context.Background(), "feed1"))
	mgr.MarkFeedUpdateFinished("feed1")
	assert.Equal(t, 1, mgr.queuedUpdates["feed1"])
	mgr.MarkFeedUpdateStarted("feed1")
	mgr.MarkFeedUpdateFinished("feed1")
	assert.Zero(t, mgr.queuedUpdates["feed1"])
	assert.False(t, mgr.runningUpdates["feed1"])
}

type mockFeedManagerDB struct {
	mu                sync.RWMutex
	episodes          map[string]map[string]*model.Episode
	walkEpisodesCount int
}

func (m *mockFeedManagerDB) Close() error          { return nil }
func (m *mockFeedManagerDB) Version() (int, error) { return 1, nil }
func (m *mockFeedManagerDB) AddFeed(_ context.Context, id string, feed *model.Feed) error {
	if m.episodes == nil {
		m.episodes = make(map[string]map[string]*model.Episode)
	}
	if m.episodes[id] == nil {
		m.episodes[id] = make(map[string]*model.Episode)
	}
	if feed != nil {
		for _, ep := range feed.Episodes {
			m.episodes[id][ep.ID] = ep
		}
	}
	return nil
}
func (m *mockFeedManagerDB) GetFeed(_ context.Context, _ string) (*model.Feed, error) {
	return nil, nil
}
func (m *mockFeedManagerDB) WalkFeeds(_ context.Context, _ func(feed *model.Feed) error) error {
	return nil
}
func (m *mockFeedManagerDB) DeleteFeed(_ context.Context, _ string) error {
	return nil
}
func (m *mockFeedManagerDB) DeleteEpisode(_ string, _ string) error {
	return nil
}
func (m *mockFeedManagerDB) GetEpisode(_ context.Context, feedID string, episodeID string) (*model.Episode, error) {
	if f, ok := m.episodes[feedID]; ok {
		if ep, ok := f[episodeID]; ok {
			return ep, nil
		}
	}
	return nil, nil
}
func (m *mockFeedManagerDB) UpdateEpisode(feedID string, episodeID string, cb func(episode *model.Episode) error) error {
	ep, _ := m.GetEpisode(context.Background(), feedID, episodeID)
	if ep != nil {
		return cb(ep)
	}
	return nil
}
func (m *mockFeedManagerDB) WalkEpisodes(_ context.Context, feedID string, cb func(episode *model.Episode) error) error {
	m.mu.Lock()
	m.walkEpisodesCount++
	epMap := m.episodes[feedID]
	eps := make([]*model.Episode, 0, len(epMap))
	for _, ep := range epMap {
		eps = append(eps, ep)
	}
	m.mu.Unlock()

	for _, ep := range eps {
		if err := cb(ep); err != nil {
			return err
		}
	}
	return nil
}
func (m *mockFeedManagerDB) SaveJob(_ context.Context, _ *model.UpdateJob) error { return nil }
func (m *mockFeedManagerDB) GetJob(_ context.Context, _ string) (*model.UpdateJob, error) {
	return nil, model.ErrNotFound
}
func (m *mockFeedManagerDB) WalkJobs(_ context.Context, _ func(job *model.UpdateJob) error) error {
	return nil
}
func (m *mockFeedManagerDB) DeleteJob(_ context.Context, _ string) error { return nil }

func TestGetFeedsStatusDoesNotTouchDBOrStorage(t *testing.T) {
	mockDB := &mockFeedManagerDB{
		episodes: map[string]map[string]*model.Episode{
			"feed_idle": {},
		},
	}
	mgr := &AppFeedManager{
		db: mockDB,
		feeds: map[string]*feed.Config{
			"feed_idle":    {ID: "feed_idle"},
			"feed_queued":  {ID: "feed_queued"},
			"feed_running": {ID: "feed_running"},
			"feed_both":    {ID: "feed_both"},
		},
		queuedUpdates: map[string]int{
			"feed_queued": 1,
			"feed_both":   2,
		},
		runningUpdates: map[string]bool{
			"feed_running": true,
			"feed_both":    true,
		},
	}

	statuses, err := mgr.GetFeedsStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, mockDB.walkEpisodesCount, "GetFeedsStatus must not touch DB or walk episodes")

	expected := []web.FeedStatusItem{
		{ID: "feed_both", Status: web.FeedStatusRunning},
		{ID: "feed_idle", Status: web.FeedStatusIdle},
		{ID: "feed_queued", Status: web.FeedStatusQueued},
		{ID: "feed_running", Status: web.FeedStatusRunning},
	}
	assert.Equal(t, expected, statuses)

	// Confirm ListFeeds does walk episodes
	_, err = mgr.ListFeeds(context.Background())
	require.NoError(t, err)
	assert.Greater(t, mockDB.walkEpisodesCount, 0, "ListFeeds should have walked episodes")
}

func TestFeedsPerformanceComparison(t *testing.T) {
	const numFeeds = 20
	const numEpisodesPerFeed = 50

	episodesMap := make(map[string]map[string]*model.Episode)
	feedsMap := make(map[string]*feed.Config)

	for i := 0; i < numFeeds; i++ {
		id := fmt.Sprintf("feed_%d", i)
		feedsMap[id] = &feed.Config{
			ID:           id,
			URL:          fmt.Sprintf("https://youtube.com/channel/%s", id),
			Format:       model.FormatAudio,
			Quality:      model.QualityHigh,
			CronSchedule: "@every 12h",
		}
		epMap := make(map[string]*model.Episode)
		for j := 0; j < numEpisodesPerFeed; j++ {
			epID := fmt.Sprintf("ep_%d_%d", i, j)
			epMap[epID] = &model.Episode{
				ID:     epID,
				Status: model.EpisodeDownloaded,
				Size:   1024 * 1024 * 10,
			}
		}
		episodesMap[id] = epMap
	}

	mockDB := &mockFeedManagerDB{
		episodes: episodesMap,
	}
	mgr := &AppFeedManager{
		db:    mockDB,
		feeds: feedsMap,
		cfg:   &Config{},
	}

	mockDB.walkEpisodesCount = 0
	startList := time.Now()
	listData, err := mgr.ListFeeds(context.Background())
	elapsedList := time.Since(startList)
	require.NoError(t, err)
	listBytes, err := json.Marshal(listData)
	require.NoError(t, err)
	listDBWalks := mockDB.walkEpisodesCount

	mockDB.walkEpisodesCount = 0
	startStatus := time.Now()
	statusData, err := mgr.GetFeedsStatus(context.Background())
	elapsedStatus := time.Since(startStatus)
	require.NoError(t, err)
	statusBytes, err := json.Marshal(statusData)
	require.NoError(t, err)
	statusDBWalks := mockDB.walkEpisodesCount

	t.Logf("\n=== Performance Comparison (%d feeds, %d total episodes) ===", numFeeds, numFeeds*numEpisodesPerFeed)
	t.Logf("ListFeeds (Old Polling):       Payload Size = %d bytes, DB Episode Walks = %d, Elapsed = %v", len(listBytes), listDBWalks, elapsedList)
	t.Logf("GetFeedsStatus (New Polling):  Payload Size = %d bytes, DB Episode Walks = %d, Elapsed = %v", len(statusBytes), statusDBWalks, elapsedStatus)

	assert.Equal(t, 0, statusDBWalks)
	assert.Equal(t, numFeeds, listDBWalks)
	assert.Less(t, len(statusBytes), len(listBytes))
}

func TestUpdateFeed_LanguageChangeRedownload(t *testing.T) {
	ep1 := &model.Episode{
		ID:     "ep1",
		Status: model.EpisodeDownloaded,
		Size:   99999,
	}

	mockDB := &mockFeedManagerDB{
		episodes: map[string]map[string]*model.Episode{
			"feed1": {
				"ep1": ep1,
			},
		},
	}

	updates := make(chan *feed.Config, 10)
	mgr := &AppFeedManager{
		cfg: &Config{
			Feeds: map[string]*feed.Config{
				"feed1": {ID: "feed1", Custom: feed.Custom{Language: "en"}},
			},
		},
		feeds: map[string]*feed.Config{
			"feed1": {ID: "feed1", Custom: feed.Custom{Language: "en"}},
		},
		db:      mockDB,
		updates: updates,
	}

	// Update feed with new language "ru"
	err := mgr.UpdateFeed(context.Background(), "feed1", &web.FeedConfigUpdate{
		Language: "ru",
	})
	assert.NoError(t, err)

	// Language in feed updated
	assert.Equal(t, "ru", mgr.feeds["feed1"].Custom.Language)

	// Episode status reset for redownload
	assert.Equal(t, model.EpisodeNew, ep1.Status)
	assert.Equal(t, int64(0), ep1.Size)

	// Update queued
	assert.Len(t, updates, 1)
}

func TestCleanTokenExtraction(t *testing.T) {
	mgr := &AppFeedManager{
		tokens: make(map[model.Provider]StringSlice),
	}

	rawVKURL := "https://oauth.vk.ru/blank.html#access_token=1234&expires_in=0&user_id=12345&email=mail@mail.com"
	err := mgr.AddTokens(context.Background(), "vkvideo", []string{rawVKURL})
	assert.NoError(t, err)

	assert.Equal(t, []string{"1234"}, []string(mgr.tokens[model.Provider("vkvideo")]))

	// UpdateTokens with another URL
	rawVKURL2 := "https://oauth.vk.com/blank.html#access_token=vk1.a.secret_key&expires_in=86400"
	err = mgr.UpdateTokens(context.Background(), "vkvideo", []string{rawVKURL2})
	assert.NoError(t, err)

	assert.Equal(t, []string{"vk1.a.secret_key"}, []string(mgr.tokens[model.Provider("vkvideo")]))
}

func TestUpdateFeed_AudiobookProtections(t *testing.T) {
	mgr := &AppFeedManager{
		cfg: &Config{
			Feeds: map[string]*feed.Config{
				"mybook": {
					ID:  "mybook",
					URL: "audiobook://Dune",
				},
			},
		},
		feeds: map[string]*feed.Config{
			"mybook": {
				ID:  "mybook",
				URL: "audiobook://Dune",
			},
		},
	}

	err := mgr.UpdateFeed(context.Background(), "mybook", &web.FeedConfigUpdate{
		KeepLast: 5,
		Format:   "video",
		Quality:  "low",
		Custom: feed.Custom{
			Title:  "Dune Updated",
			Author: "Frank Herbert",
		},
	})
	assert.NoError(t, err)

	updated := mgr.feeds["mybook"]
	assert.Nil(t, updated.Clean)
	assert.Equal(t, model.FormatAudio, updated.Format)
	assert.Equal(t, model.QualityHigh, updated.Quality)
	assert.Equal(t, "Dune Updated", updated.Custom.Title)
	assert.Equal(t, "Frank Herbert", updated.Custom.Author)
}

func TestUpdateFeed_PrivateFeedPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	initialTOML := "[server]\nport = 8080\n\n[storage.local]\ndata_dir = \"/tmp\"\n\n[feeds.mybook]\nurl = \"audiobook://Dune\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(initialTOML), 0644))

	bookCfg := &feed.Config{
		ID:  "mybook",
		URL: "audiobook://Dune",
	}

	updates := make(chan *feed.Config, 10)
	mgr := &AppFeedManager{
		configPath: configPath,
		cfg: &Config{
			Feeds: map[string]*feed.Config{
				"mybook": bookCfg,
			},
		},
		feeds: map[string]*feed.Config{
			"mybook": bookCfg,
		},
		updates: updates,
	}

	err := mgr.UpdateFeed(context.Background(), "mybook", &web.FeedConfigUpdate{
		PrivateFeed: true,
	})
	require.NoError(t, err)

	assert.True(t, mgr.feeds["mybook"].PrivateFeed)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	t.Logf("Config content:\n%s", string(content))
	assert.Contains(t, string(content), "private_feed = true")

	reloaded, err := LoadConfig(configPath)
	require.NoError(t, err)
	require.NotNil(t, reloaded.Feeds["mybook"])
	assert.True(t, reloaded.Feeds["mybook"].PrivateFeed)
}

func TestUpdateFeedWhileQueuedUpdateBuildsXML(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[feeds.test]\nurl = \"https://example.com/feed\"\n"), 0600))
	initial := &feed.Config{ID: "test", URL: "https://example.com/feed", Custom: feed.Custom{Title: "Before"}}
	updates := make(chan *feed.Config, 2)
	mgr := &AppFeedManager{
		configPath: configPath,
		cfg:        &Config{Feeds: map[string]*feed.Config{"test": initial}},
		feeds:      map[string]*feed.Config{"test": initial},
		updates:    updates,
	}
	require.NoError(t, mgr.TriggerUpdate(context.Background(), "test"))
	inFlight := <-updates
	started := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan string, 1)
	go func() {
		close(started)
		<-finish
		podcast, err := feed.Build(context.Background(), &model.Feed{Title: "Source"}, inFlight, "http://localhost")
		if err != nil {
			done <- err.Error()
			return
		}
		done <- feed.FormatXML(podcast, &model.Feed{})
	}()
	<-started
	require.NoError(t, mgr.UpdateFeed(context.Background(), "test", &web.FeedConfigUpdate{
		PrivateFeed: true,
		Custom:      feed.Custom{Title: "After"},
	}))
	close(finish)
	oldXML := <-done
	assert.Contains(t, oldXML, "Before")
	assert.NotContains(t, oldXML, "<itunes:block>yes</itunes:block>")
	current := <-updates
	newPodcast, err := feed.Build(context.Background(), &model.Feed{Title: "Source"}, current, "http://localhost")
	require.NoError(t, err)
	newXML := feed.FormatXML(newPodcast, &model.Feed{})
	assert.Contains(t, newXML, "After")
	assert.Contains(t, newXML, "<itunes:block>yes</itunes:block>")
	stored, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(stored), "private_feed = true")
}

func TestUpdateFeedFailedSaveKeepsCurrentConfig(t *testing.T) {
	initial := &feed.Config{ID: "test", URL: "https://example.com/feed", Custom: feed.Custom{Title: "Before"}}
	mgr := &AppFeedManager{
		configPath: filepath.Join(t.TempDir(), "missing", "config.toml"),
		cfg:        &Config{Feeds: map[string]*feed.Config{"test": initial}},
		feeds:      map[string]*feed.Config{"test": initial},
		updates:    make(chan *feed.Config, 1),
	}
	err := mgr.UpdateFeed(context.Background(), "test", &web.FeedConfigUpdate{Custom: feed.Custom{Title: "After"}})
	require.Error(t, err)
	assert.Same(t, initial, mgr.feeds["test"])
	assert.Equal(t, "Before", mgr.cfg.Feeds["test"].Custom.Title)
	assert.Empty(t, mgr.updates)
}

func TestDeleteAudiobookDir(t *testing.T) {
	tmpDir := t.TempDir()
	booksDir := filepath.Join(tmpDir, "audiobooks")
	require.NoError(t, os.MkdirAll(booksDir, 0755))

	// Create test book folder
	book1Dir := filepath.Join(booksDir, "Book1")
	require.NoError(t, os.MkdirAll(book1Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(book1Dir, "01.mp3"), []byte("dummy audio"), 0644))

	configPath := filepath.Join(tmpDir, "config.toml")
	cfgTOML := "[server]\nport = 8080\n\n[audiobooks]\ndir = \"" + booksDir + "\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(cfgTOML), 0644))

	mgr := &AppFeedManager{
		configPath: configPath,
		cfg: &Config{
			Audiobooks: AudiobooksConfig{
				Dir: booksDir,
			},
		},
		feeds: map[string]*feed.Config{},
	}

	// 1. Path traversal check
	err := mgr.DeleteAudiobookDir(context.Background(), "../secret")
	assert.Error(t, err)

	// 2. Delete existing unlinked book
	err = mgr.DeleteAudiobookDir(context.Background(), "Book1")
	require.NoError(t, err)

	_, err = os.Stat(book1Dir)
	assert.True(t, os.IsNotExist(err))

	// 3. Delete non-existent book
	err = mgr.DeleteAudiobookDir(context.Background(), "NonExistent")
	assert.Error(t, err)
}

func TestGetFeedDetail_AudiobookDynamicScan(t *testing.T) {
	tmpDir := t.TempDir()
	booksDir := filepath.Join(tmpDir, "audiobooks")
	require.NoError(t, os.MkdirAll(booksDir, 0755))

	bookDir := filepath.Join(booksDir, "Dune")
	require.NoError(t, os.MkdirAll(bookDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bookDir, "01_intro.mp3"), []byte("mp3 content 1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(bookDir, "02_chapter.mp3"), []byte("mp3 content 2"), 0644))

	mockDB := &mockFeedManagerDB{
		episodes: make(map[string]map[string]*model.Episode),
	}

	bookFeed := &feed.Config{
		ID:  "dune_book",
		URL: "audiobook://Dune",
	}

	mgr := &AppFeedManager{
		cfg: &Config{
			Audiobooks: AudiobooksConfig{
				Dir: booksDir,
			},
		},
		feeds: map[string]*feed.Config{
			"dune_book": bookFeed,
		},
		db: mockDB,
	}

	summary, episodes, err := mgr.GetFeedDetail(context.Background(), "dune_book")
	require.NoError(t, err)
	assert.Equal(t, 2, summary.EpisodeCount)
	require.Len(t, episodes, 2)
	assert.Equal(t, "01_intro.mp3", episodes[0].ID)
	assert.Equal(t, "02_chapter.mp3", episodes[1].ID)
}

func TestAudiobookUpdatesWithoutCron(t *testing.T) {
	ctx := context.Background()
	updates := make(chan *feed.Config, 4)
	mgr := &AppFeedManager{
		feeds:       make(map[string]*feed.Config),
		updates:     updates,
		cron:        cron.New(),
		cronEntries: make(map[string]cron.EntryID),
	}
	book := &feed.Config{ID: "book", URL: "audiobook://Dune", CronSchedule: "@every 1h"}
	require.NoError(t, mgr.AddFeed(ctx, book))
	assert.Empty(t, book.CronSchedule)
	assert.Empty(t, mgr.cron.Entries())
	require.Len(t, updates, 1)
	assert.Equal(t, "book", (<-updates).ID)

	require.NoError(t, mgr.TriggerUpdate(ctx, "book"))
	require.Len(t, updates, 1)
	<-updates

	// Remove a legacy schedule when saving book settings.
	entry, err := mgr.cron.AddFunc("@every 1h", func() {})
	require.NoError(t, err)
	mgr.cronEntries["book"] = entry
	require.NoError(t, mgr.UpdateFeed(ctx, "book", &web.FeedConfigUpdate{
		CronSchedule: "@every 2h",
		Custom:       feed.Custom{Title: "Updated book"},
	}))
	assert.Empty(t, mgr.feeds["book"].CronSchedule)
	assert.Empty(t, mgr.cron.Entries())
	require.Len(t, updates, 1)
	assert.Equal(t, "Updated book", (<-updates).Custom.Title)

	require.NoError(t, mgr.AddFeed(ctx, &feed.Config{ID: "channel", URL: "https://www.youtube.com/channel/UCtest"}))
	assert.Equal(t, "@every 12h", mgr.feeds["channel"].CronSchedule)
	assert.Len(t, mgr.cron.Entries(), 1)
}

func TestInvalidScheduleAndFullQueueDoNotChangeFeed(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	initial := "[feeds.channel]\nurl = \"https://youtube.com/channel/UCtest\"\ncron_schedule = \"@every 1h\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0600))
	cfg := &feed.Config{ID: "channel", URL: "https://youtube.com/channel/UCtest", CronSchedule: "@every 1h", PrivateFeed: true}
	mgr := &AppFeedManager{configPath: configPath, feeds: map[string]*feed.Config{"channel": cfg},
		updates: make(chan *feed.Config, 1), cron: cron.New(), cronEntries: make(map[string]cron.EntryID)}
	entry, err := mgr.cron.AddFunc(cfg.CronSchedule, func() {})
	require.NoError(t, err)
	mgr.cronEntries[cfg.ID] = entry
	var patch web.FeedConfigUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"cron_schedule":"nonsense"}`), &patch))
	require.Error(t, mgr.UpdateFeed(ctx, cfg.ID, &patch))
	assert.Equal(t, entry, mgr.cronEntries[cfg.ID])
	assert.Same(t, cfg, mgr.feeds[cfg.ID])

	mgr.updates <- cfg
	require.NoError(t, json.Unmarshal([]byte(`{"custom":{"title":"Changed"}}`), &patch))
	require.ErrorContains(t, mgr.UpdateFeed(ctx, cfg.ID, &patch), "queue is full")
	require.Error(t, mgr.AddFeed(ctx, &feed.Config{ID: "new", URL: cfg.URL}))
	assert.NotContains(t, mgr.feeds, "new")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, initial, string(data))
	assert.Empty(t, cfg.Custom.Title)
}

func TestImportAudiobookRejectsEmptyAndOutsideFolders(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0755))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "outside")))
	mgr := &AppFeedManager{cfg: &Config{Audiobooks: AudiobooksConfig{Dir: root}},
		feeds: make(map[string]*feed.Config), updates: make(chan *feed.Config, 2)}
	for _, folder := range []string{"empty", "outside"} {
		_, err := mgr.ImportAudiobook(context.Background(), &web.ImportAudiobookRequest{DirName: folder, ID: folder})
		require.Error(t, err)
	}
	assert.Empty(t, mgr.feeds)
	assert.Empty(t, mgr.updates)
}

func TestAudiobookUploadRetryAcceptsIdenticalFiles(t *testing.T) {
	mgr := &AppFeedManager{cfg: &Config{Audiobooks: AudiobooksConfig{Dir: t.TempDir()}}}
	require.NoError(t, mgr.SaveAudiobookFile("book", "01.mp3", strings.NewReader("audio")))
	require.NoError(t, mgr.SaveAudiobookFile("book", "01.mp3", strings.NewReader("audio")))
	require.Error(t, mgr.SaveAudiobookFile("book", "01.mp3", strings.NewReader("different")))
}

func TestEpisodeDetailOrderAndMediaLinks(t *testing.T) {
	now := time.Now()
	mgr := &AppFeedManager{
		cfg:   &Config{Server: web.Config{Hostname: "https://example.com", Path: "podcasts"}},
		feeds: map[string]*feed.Config{"show": {ID: "show", URL: "https://youtube.com/channel/UCtest", Format: model.FormatAudio}},
		db: &mockFeedManagerDB{episodes: map[string]map[string]*model.Episode{"show": {
			"old": {ID: "old", PubDate: now.Add(-time.Hour), Status: model.EpisodeCleaned},
			"new": {ID: "new", Title: "Newest", PubDate: now, Status: model.EpisodeDownloaded},
		}}},
	}
	_, episodes, err := mgr.GetFeedDetail(context.Background(), "show")
	require.NoError(t, err)
	require.Len(t, episodes, 2)
	assert.Equal(t, "new", episodes[0].ID)
	assert.Contains(t, episodes[0].MediaURL, "https://example.com/podcasts/show/")
	assert.Empty(t, episodes[1].MediaURL)
}

func TestConcurrentAPIWhileFeedUpdates(t *testing.T) {
	mockDB := &mockFeedManagerDB{
		episodes: map[string]map[string]*model.Episode{
			"feed1": {
				"ep1": {ID: "ep1", Status: model.EpisodeDownloaded, Size: 1000},
				"ep2": {ID: "ep2", Status: model.EpisodeError, Error: "timeout"},
			},
		},
	}
	mgr := &AppFeedManager{
		cfg: &Config{
			Server:     web.Config{Hostname: "https://example.com"},
			Storage:    fs.Config{Type: "local", Local: fs.LocalConfig{DataDir: t.TempDir()}},
			Downloader: ytdl.Config{Timeout: 10},
		},
		feeds: map[string]*feed.Config{
			"feed1": {ID: "feed1", URL: "https://youtube.com/channel/test", Format: model.FormatAudio},
		},
		db:             mockDB,
		queuedUpdates:  make(map[string]int),
		runningUpdates: make(map[string]bool),
		feedErrors:     make(map[string]feedErrorEntry),
	}

	stop := make(chan struct{})
	// Goroutine simulating background update cycle (acquiring write lock)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				mgr.MarkFeedUpdateQueued("feed1")
				mgr.MarkFeedUpdateStarted("feed1")
				mgr.RecordFeedUpdateError("feed1", fmt.Errorf("network blip"))
				mgr.ClearFeedUpdateError("feed1")
				mgr.MarkFeedUpdateFinished("feed1")
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Run concurrent API readers
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					status, err := mgr.GetFeedsStatus(ctx)
					assert.NoError(t, err)
					assert.NotEmpty(t, status)

					feeds, err := mgr.ListFeeds(ctx)
					assert.NoError(t, err)
					assert.NotEmpty(t, feeds)

					syncStatus, err := mgr.GetSyncStatus(ctx)
					assert.NoError(t, err)
					assert.NotNil(t, syncStatus)

					errs, err := mgr.GetDownloadErrors(ctx, "", 10)
					assert.NoError(t, err)
					assert.NotNil(t, errs)

					dlCfg, err := mgr.GetDownloaderConfig(ctx)
					assert.NoError(t, err)
					assert.NotNil(t, dlCfg)
				}
			}
		}()
	}

	<-ctx.Done()
	close(stop)
	wg.Wait()
}

func TestFeedManager_TriggerUpdateMultipleTimesNoCounterLeak(t *testing.T) {
	ctx := context.Background()
	storage, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	q := jobqueue.NewJobQueue(storage, 5)
	mgr := &AppFeedManager{
		queue:          q,
		feeds:          map[string]*feed.Config{"f1": {ID: "f1", URL: "https://youtube.com/channel/test"}},
		queuedUpdates:  make(map[string]int),
		runningUpdates: make(map[string]bool),
		updates:        make(chan *feed.Config, 10),
	}

	// Trigger 5 times concurrently or sequentially
	for i := 0; i < 5; i++ {
		require.NoError(t, mgr.TriggerUpdate(ctx, "f1"))
	}

	// Counter must be exactly 1, not 5
	assert.Equal(t, 1, mgr.queuedUpdates["f1"])

	// Claim and start
	job, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)

	started := mgr.TryStartFeedUpdate("f1")
	assert.True(t, started)
	assert.Equal(t, 0, mgr.queuedUpdates["f1"])

	// Finish update
	mgr.MarkFeedUpdateFinished("f1")
	require.NoError(t, q.MarkDone(ctx, job.ID, job.ClaimToken))

	status, err := mgr.GetFeedsStatus(ctx)
	require.NoError(t, err)
	require.Len(t, status, 1)
	assert.Equal(t, web.FeedStatusIdle, status[0].Status)
}

func TestFeedManager_TriggerUpdateDoesNotPushToChannelWhenQueueActive(t *testing.T) {
	ctx := context.Background()
	storage, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	q := jobqueue.NewJobQueue(storage, 5)
	updates := make(chan *feed.Config, 10)
	mgr := &AppFeedManager{
		queue:          q,
		feeds:          map[string]*feed.Config{"f1": {ID: "f1", URL: "https://youtube.com/channel/test"}},
		queuedUpdates:  make(map[string]int),
		runningUpdates: make(map[string]bool),
		updates:        updates,
	}

	require.NoError(t, mgr.TriggerUpdate(ctx, "f1"))
	// Must NOT send into updates channel to prevent duplicate processing by legacy listener
	assert.Empty(t, updates)

	// But job must be present in persistent queue
	job, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "update/f1", job.ID)
}

func TestFeedManager_TriggerUpdateAllNoDuplicateJobWhenClaimed(t *testing.T) {
	ctx := context.Background()
	storage, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	q := jobqueue.NewJobQueue(storage, 5)
	updates := make(chan *feed.Config, 10)
	mgr := &AppFeedManager{
		queue:          q,
		feeds:          map[string]*feed.Config{"f1": {ID: "f1", URL: "https://youtube.com/channel/test"}},
		queuedUpdates:  make(map[string]int),
		runningUpdates: make(map[string]bool),
		updates:        updates,
	}

	// 1. Simulate legacy channel ingestion listener (main.go:273) in background
	stopListener := make(chan struct{})
	defer close(stopListener)
	go func() {
		for {
			select {
			case f := <-updates:
				if f != nil {
					_, _, _ = q.EnqueueUpdate(ctx, f.ID)
				}
			case <-stopListener:
				return
			}
		}
	}()

	// 2. User clicks "Update all"
	queued, err := mgr.TriggerUpdateAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"f1"}, queued)

	// 3. Worker immediately claims the job
	job, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "update/f1", job.ID)
	assert.Equal(t, model.JobStatusRunning, job.Status)

	// Wait briefly to confirm no rogue message arrived through updates channel to set PendingRerun
	time.Sleep(50 * time.Millisecond)
	currentJob, err := q.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.False(t, currentJob.PendingRerun)

	// 4. Worker completes the job
	require.NoError(t, q.MarkDone(ctx, job.ID, job.ClaimToken))

	// 5. Job must be DONE, not re-queued!
	finalJob, err := q.GetJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusDone, finalJob.Status)
}

func TestSaveCoverImage(t *testing.T) {
	dataDir := t.TempDir()
	storage, err := fs.NewLocal(dataDir, false, false)
	require.NoError(t, err)

	mgr := &AppFeedManager{
		cfg: &Config{
			Server:  web.Config{Hostname: "https://example.com"},
			Storage: fs.Config{Type: "local", Local: fs.LocalConfig{DataDir: dataDir}},
		},
		storage: storage,
	}

	// 1. With feed ID
	res, err := mgr.SaveCoverImage(context.Background(), "my_feed", "podcast.PNG", strings.NewReader("fake-png-content"))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/my_feed/cover.png", res.URL)
	assert.Equal(t, "/my_feed/cover.png", res.RelURL)
	assert.Equal(t, "my_feed", res.FeedID)
	assert.Equal(t, "cover.png", res.Filename)

	savedData, err := os.ReadFile(filepath.Join(dataDir, "my_feed", "cover.png"))
	require.NoError(t, err)
	assert.Equal(t, "fake-png-content", string(savedData))

	// 2. Without feed ID (generic covers/)
	res2, err := mgr.SaveCoverImage(context.Background(), "", "art.jpg", strings.NewReader("fake-jpg-content"))
	require.NoError(t, err)
	assert.Contains(t, res2.URL, "https://example.com/covers/cover_")
	assert.Contains(t, res2.RelURL, "/covers/cover_")
	assert.Equal(t, "", res2.FeedID)
	assert.True(t, strings.HasSuffix(res2.Filename, ".jpg"))
}
