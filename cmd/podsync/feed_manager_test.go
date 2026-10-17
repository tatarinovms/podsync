package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
)

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

type mockFeedManagerDB struct {
	episodes map[string]map[string]*model.Episode
}

func (m *mockFeedManagerDB) Close() error          { return nil }
func (m *mockFeedManagerDB) Version() (int, error) { return 1, nil }
func (m *mockFeedManagerDB) AddFeed(_ context.Context, _ string, _ *model.Feed) error {
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
	for _, ep := range m.episodes[feedID] {
		if err := cb(ep); err != nil {
			return err
		}
	}
	return nil
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
