package main

import (
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
