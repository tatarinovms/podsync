package web

import (
	"encoding/json"
	"testing"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedPatchPreservesOmittedSettingsAcrossProxy(t *testing.T) {
	current := &feed.Config{PrivateFeed: true, MaxHeight: 720,
		Clean:   &feed.Cleanup{KeepLast: 8},
		Custom:  feed.Custom{Title: "Book", Author: "Author", Language: "ru"},
		Filters: feed.Filters{Title: "interview"}, YouTubeDLArgs: []string{"--verbose"}}
	var patch FeedConfigUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"cron_schedule":"@every 2h"}`), &patch))
	// MCP HTTP forwarding must not turn omitted fields into zero values.
	payload, err := json.Marshal(patch)
	require.NoError(t, err)
	var forwarded FeedConfigUpdate
	require.NoError(t, json.Unmarshal(payload, &forwarded))
	resolved, err := forwarded.WithCurrent(current)
	require.NoError(t, err)
	assert.True(t, resolved.PrivateFeed)
	assert.Equal(t, 8, resolved.KeepLast)
	assert.Equal(t, current.Custom, resolved.Custom)
	assert.Equal(t, current.Filters, resolved.Filters)
	assert.Equal(t, current.YouTubeDLArgs, resolved.YouTubeDLArgs)
	assert.Equal(t, "@every 2h", resolved.CronSchedule)

	require.NoError(t, json.Unmarshal([]byte(`{"private_feed":false,"max_height":0,"keep_last":0,"custom":{"title":""},"language":"","filters":{},"youtube_dl_args":[]}`), &patch))
	resolved, err = patch.WithCurrent(current)
	require.NoError(t, err)
	assert.False(t, resolved.PrivateFeed)
	assert.Zero(t, resolved.MaxHeight)
	assert.Zero(t, resolved.KeepLast)
	assert.Empty(t, resolved.Custom.Title)
	assert.Empty(t, resolved.Custom.Language)
	assert.Equal(t, "Author", resolved.Custom.Author)
	assert.Empty(t, resolved.Filters)
	assert.Empty(t, resolved.YouTubeDLArgs)
	require.Error(t, json.Unmarshal([]byte(`null`), &patch))
}
