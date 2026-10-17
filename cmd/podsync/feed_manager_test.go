package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mxpv/podsync/pkg/feed"
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
