package builder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel>
    <title>BeardyCast</title>
    <link>https://beardycast.com</link>
    <description>Weekly technology podcast</description>
    <language>ru</language>
    <itunes:author>Beardy Network</itunes:author>
    <itunes:image href="https://beardycast.com/cover.jpg"/>
    <item>
      <title>Episode 400: Future of Podsync</title>
      <guid isPermaLink="false">bc-400</guid>
      <pubDate>Mon, 20 Oct 2026 12:00:00 +0000</pubDate>
      <description>&lt;p&gt;Discussion about podcast archiving and RSS mirroring.&lt;/p&gt;</description>
      <itunes:duration>01:23:45</itunes:duration>
      <enclosure url="https://cdn.beardycast.com/episodes/bc400.mp3" length="60384120" type="audio/mpeg"/>
    </item>
    <item>
      <title>Episode 399: Hardware special</title>
      <guid isPermaLink="false">bc-399</guid>
      <pubDate>Mon, 13 Oct 2026 10:00:00 +0000</pubDate>
      <description>Hardware deep dive</description>
      <itunes:duration>45:12</itunes:duration>
      <enclosure url="https://cdn.beardycast.com/episodes/bc399.m4a" length="42100000" type="audio/x-m4a"/>
    </item>
  </channel>
</rss>`

func TestParseRSSStream(t *testing.T) {
	reader := strings.NewReader(sampleRSS)
	meta, err := ParseRSSStream(reader, "https://beardycast.com/feed/")
	require.NoError(t, err)

	assert.Equal(t, "BeardyCast", meta.Title)
	assert.Equal(t, "Beardy Network", meta.Author)
	assert.Equal(t, "Weekly technology podcast", meta.Description)
	assert.Equal(t, "https://beardycast.com/cover.jpg", meta.CoverArt)
	assert.Equal(t, 2, meta.TotalEpisodes)
	assert.Equal(t, int64(60384120+42100000), meta.TotalBytes)
	require.NotNil(t, meta.LatestPubDate)
	assert.Equal(t, 2026, meta.LatestPubDate.Year())

	require.Len(t, meta.Episodes, 2)
	ep1 := meta.Episodes[0]
	assert.Equal(t, "bc-400", ep1.ID)
	assert.Equal(t, "Episode 400: Future of Podsync", ep1.Title)
	assert.Equal(t, "Discussion about podcast archiving and RSS mirroring.", ep1.Description)
	assert.Equal(t, int64(5025), ep1.Duration) // 1h 23m 45s = 5025s
	assert.Equal(t, "https://cdn.beardycast.com/episodes/bc400.mp3", ep1.VideoURL)
	assert.Equal(t, "https://cdn.beardycast.com/episodes/bc400.mp3", ep1.MediaURL)
	assert.Equal(t, int64(60384120), ep1.Size)
	assert.Equal(t, model.EpisodeNew, ep1.Status)

	ep2 := meta.Episodes[1]
	assert.Equal(t, "bc-399", ep2.ID)
	assert.Equal(t, int64(2712), ep2.Duration) // 45m 12s = 2712s
}

func TestParseURL_RSS(t *testing.T) {
	tests := []struct {
		url      string
		expected model.Provider
	}{
		{"https://beardycast.com/feed/", model.ProviderRSS},
		{"https://podcast.example.com/rss.xml", model.ProviderRSS},
		{"http://feeds.feedburner.com/myfeed", model.ProviderRSS},
		{"https://news.ycombinator.com/rss", model.ProviderRSS},
		{"rss://myhost.com/podcast", model.ProviderRSS},
		{"rss+https://myhost.com/podcast/audio.xml", model.ProviderRSS},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			info, err := ParseURL(tt.url)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info.Provider)
			assert.Equal(t, model.TypeRSS, info.LinkType)
			assert.NotEmpty(t, info.ItemID)
		})
	}
}

func TestDetectMediaExtension(t *testing.T) {
	assert.Equal(t, "mp3", DetectMediaExtension("https://cdn.com/audio.mp3?token=123", "audio/mpeg"))
	assert.Equal(t, "m4a", DetectMediaExtension("https://cdn.com/audio.m4a", "audio/x-m4a"))
	assert.Equal(t, "aac", DetectMediaExtension("https://cdn.com/stream", "audio/aac"))
	assert.Equal(t, "ogg", DetectMediaExtension("https://cdn.com/stream", "audio/ogg"))
	assert.Equal(t, "mp3", DetectMediaExtension("https://cdn.com/stream", ""))
}

func TestRSSBuilder_Build_PageSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()

	builder := NewRSSBuilder()

	// 1. PageSize = 1, Descending (default): should return only episode 400
	feedDesc, err := builder.Build(context.Background(), &feed.Config{
		ID:       "test-feed",
		URL:      srv.URL,
		PageSize: 1,
	})
	require.NoError(t, err)
	require.Len(t, feedDesc.Episodes, 1)
	assert.Equal(t, "bc-400", feedDesc.Episodes[0].ID)

	// 2. PageSize = 1, Ascending: should return only episode 399 (oldest)
	feedAsc, err := builder.Build(context.Background(), &feed.Config{
		ID:           "test-feed",
		URL:          srv.URL,
		PageSize:     1,
		PlaylistSort: model.SortingAsc,
	})
	require.NoError(t, err)
	require.Len(t, feedAsc.Episodes, 1)
	assert.Equal(t, "bc-399", feedAsc.Episodes[0].ID)

	// 3. PageSize = 0 (All episodes): should return all 2 episodes
	feedAll, err := builder.Build(context.Background(), &feed.Config{
		ID:       "test-feed",
		URL:      srv.URL,
		PageSize: 0,
	})
	require.NoError(t, err)
	require.Len(t, feedAll.Episodes, 2)
}
