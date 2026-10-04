package builder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

func TestVkVideoBuilder_MissingToken(t *testing.T) {
	b, err := NewVkVideoBuilder("", nil)
	require.NoError(t, err)

	cfg := &feed.Config{
		URL:      "https://vkvideo.ru/@labelcom",
		PageSize: 10,
	}

	_, err = b.Build(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "VK API token is required")
}

func TestVkVideoBuilder_GroupFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path
		w.Header().Set("Content-Type", "application/json")

		switch method {
		case "/utils.resolveScreenName":
			require.Equal(t, "labelcom", r.URL.Query().Get("screen_name"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"type":      "group",
					"object_id": 22822305,
				},
			})
		case "/groups.getById":
			require.Equal(t, "22822305", r.URL.Query().Get("group_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": []map[string]interface{}{
					{
						"id":          22822305,
						"name":        "LABELCOM &amp; Co",
						"screen_name": "labelcom",
						"description": "Official &quot;Labelcom&quot; Community",
						"photo_200":   "https://sun9-1.userapi.com/labelcom_photo.jpg",
					},
				},
			})
		case "/video.get":
			require.Equal(t, "-22822305", r.URL.Query().Get("owner_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"count": 4,
					"items": []map[string]interface{}{
						{
							"id":          1001,
							"owner_id":    -22822305,
							"title":       "&quot;Show Episode #1&quot;",
							"description": "Hilarious &amp; fun episode description",
							"duration":    3600,
							"date":        1700000000,
							"live":        0,
							"upcoming":    0,
							"access_key":  "secret123",
							"image": []map[string]interface{}{
								{"url": "https://thumb-small.jpg", "width": 320, "height": 180},
								{"url": "https://thumb-big.jpg", "width": 1280, "height": 720},
							},
						},
						{
							// Processing video - must be skipped
							"id":         1004,
							"owner_id":   -22822305,
							"title":      "Processing Video",
							"duration":   500,
							"processing": 1,
						},
						{
							// Upcoming stream - must be skipped
							"id":          1002,
							"owner_id":    -22822305,
							"title":       "Upcoming Stream",
							"description": "Not yet started",
							"duration":    0,
							"date":        1700001000,
							"live":        1,
							"upcoming":    1,
							"live_status": "upcoming",
						},
						{
							// Ongoing active live - must be skipped
							"id":          1003,
							"owner_id":    -22822305,
							"title":       "Active Live Broadcast",
							"description": "Currently streaming",
							"duration":    0,
							"date":        1700002000,
							"live":        1,
							"upcoming":    0,
							"live_status": "started",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	b, err := NewVkVideoBuilder("test_token", nil)
	require.NoError(t, err)
	b.client.apiURL = server.URL + "/"

	cfg := &feed.Config{
		URL:      "https://vkvideo.ru/@labelcom",
		PageSize: 10,
		Format:   model.FormatVideo,
		Quality:  model.QualityHigh,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)

	// HTML unescaping test
	require.Equal(t, "LABELCOM & Co", result.Title)
	require.Equal(t, "Official \"Labelcom\" Community", result.Description)
	require.Equal(t, "LABELCOM & Co", result.Author)
	require.Equal(t, "https://sun9-1.userapi.com/labelcom_photo.jpg", result.CoverArt)
	require.Equal(t, "https://vkvideo.ru/@labelcom", result.ItemURL)

	// Upcoming, live streams, and processing videos skipped: only 1 video added
	require.Len(t, result.Episodes, 1)

	ep := result.Episodes[0]
	require.Equal(t, "-22822305_1001", ep.ID)
	require.Equal(t, "\"Show Episode #1\"", ep.Title)
	require.Equal(t, "Hilarious & fun episode description", ep.Description)
	require.Equal(t, int64(3600), ep.Duration)
	require.Equal(t, "https://vk.com/video-22822305_1001?access_key=secret123", ep.VideoURL)
	require.Equal(t, "https://thumb-big.jpg", ep.Thumbnail)
	require.Equal(t, time.Unix(1700000000, 0).UTC(), ep.PubDate)
	require.Equal(t, model.EpisodeNew, ep.Status)
	require.True(t, ep.Size > 0)
}

func TestVkVideoBuilder_UserFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path
		w.Header().Set("Content-Type", "application/json")

		switch method {
		case "/users.get":
			require.Equal(t, "12345", r.URL.Query().Get("user_ids"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": []map[string]interface{}{
					{
						"id":          12345,
						"first_name":  "Pavel",
						"last_name":   "Durov",
						"screen_name": "durov",
						"about":       "Creator of Telegram",
						"photo_200":   "https://vk.com/durov.jpg",
					},
				},
			})
		case "/video.get":
			require.Equal(t, "12345", r.URL.Query().Get("owner_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"count": 1,
					"items": []map[string]interface{}{
						{
							"id":          500,
							"owner_id":    12345,
							"title":       "Speech in Paris",
							"description": "Tech conference talk",
							"duration":    1200,
							"date":        1690000000,
							"photo_320":   "https://thumb.jpg",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	b, err := NewVkVideoBuilder("test_token", nil)
	require.NoError(t, err)
	b.client.apiURL = server.URL + "/"

	cfg := &feed.Config{
		URL:      "https://vk.com/id12345",
		PageSize: 10,
		Format:   model.FormatAudio,
		Quality:  model.QualityHigh,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "Pavel Durov", result.Title)
	require.Equal(t, "Creator of Telegram", result.Description)
	require.Equal(t, "Pavel Durov", result.Author)
	require.Equal(t, "https://vk.com/durov.jpg", result.CoverArt)
	require.Equal(t, "https://vkvideo.ru/@durov", result.ItemURL)

	require.Len(t, result.Episodes, 1)
	ep := result.Episodes[0]
	require.Equal(t, "12345_500", ep.ID)
	require.Equal(t, "Speech in Paris", ep.Title)
	require.Equal(t, "https://vk.com/video12345_500", ep.VideoURL)
	require.Equal(t, "https://thumb.jpg", ep.Thumbnail)
}

func TestVkVideoBuilder_PlaylistFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path
		w.Header().Set("Content-Type", "application/json")

		switch method {
		case "/video.getAlbumById":
			require.Equal(t, "-22822305", r.URL.Query().Get("owner_id"))
			require.Equal(t, "777", r.URL.Query().Get("album_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"id":          777,
					"owner_id":    -22822305,
					"title":       "Best Moments Season 1",
					"description": "Collection of season 1 highlights",
					"count":       1,
					"image": []map[string]interface{}{
						{"url": "https://album_cover.jpg", "width": 800, "height": 600},
					},
				},
			})
		case "/groups.getById":
			require.Equal(t, "22822305", r.URL.Query().Get("group_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": []map[string]interface{}{
					{
						"id":          22822305,
						"name":        "LABELCOM",
						"screen_name": "labelcom",
						"photo_200":   "https://author_photo.jpg",
					},
				},
			})
		case "/video.get":
			require.Equal(t, "-22822305", r.URL.Query().Get("owner_id"))
			require.Equal(t, "777", r.URL.Query().Get("album_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"count": 1,
					"items": []map[string]interface{}{
						{
							"id":       999,
							"owner_id": -22822305,
							"title":    "Highlight #1",
							"duration": 300,
							"date":     1710000000,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	b, err := NewVkVideoBuilder("test_token", nil)
	require.NoError(t, err)
	b.client.apiURL = server.URL + "/"

	cfg := &feed.Config{
		URL:      "https://vkvideo.ru/playlist/-22822305_777",
		PageSize: 10,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "Best Moments Season 1", result.Title)
	require.Equal(t, "Collection of season 1 highlights", result.Description)
	require.Equal(t, "LABELCOM", result.Author)
	require.Equal(t, "https://album_cover.jpg", result.CoverArt)
	require.Equal(t, "https://vkvideo.ru/playlist/-22822305_777", result.ItemURL)

	require.Len(t, result.Episodes, 1)
	require.Equal(t, "-22822305_999", result.Episodes[0].ID)
}

func TestVkVideoBuilder_SortingAsc(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/groups.getById":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": []map[string]interface{}{
					{"id": 100, "name": "Test Group", "screen_name": "test"},
				},
			})
		case "/video.get":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"count": 2,
					"items": []map[string]interface{}{
						{"id": 2, "owner_id": -100, "title": "Second", "duration": 100, "date": 200},
						{"id": 1, "owner_id": -100, "title": "First", "duration": 100, "date": 100},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	b, err := NewVkVideoBuilder("test_token", nil)
	require.NoError(t, err)
	b.client.apiURL = server.URL + "/"

	cfg := &feed.Config{
		URL:          "https://vk.com/club100",
		PageSize:     10,
		PlaylistSort: model.SortingAsc,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, result.Episodes, 2)
	require.Equal(t, "First", result.Episodes[0].Title)
	require.Equal(t, "Second", result.Episodes[1].Title)
}

func TestVkVideoBuilder_EventCommunity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/utils.resolveScreenName":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"type":      "event",
					"object_id": 999000,
				},
			})
		case "/groups.getById":
			require.Equal(t, "999000", r.URL.Query().Get("group_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": []map[string]interface{}{
					{"id": 999000, "name": "Rock Fest 2026", "screen_name": "rockfest2026"},
				},
			})
		case "/video.get":
			require.Equal(t, "-999000", r.URL.Query().Get("owner_id"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"response": map[string]interface{}{
					"count": 1,
					"items": []map[string]interface{}{
						{"id": 1, "owner_id": -999000, "title": "Fest Intro", "duration": 120, "date": 100},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	b, err := NewVkVideoBuilder("test_token", nil)
	require.NoError(t, err)
	b.client.apiURL = server.URL + "/"

	cfg := &feed.Config{
		URL:      "https://vk.com/rockfest2026",
		PageSize: 10,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, "Rock Fest 2026", result.Title)
	require.Len(t, result.Episodes, 1)
}

func TestVkVideoBuilder_RateLimitRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cur := atomic.AddInt32(&attempts, 1)
		if cur == 1 {
			// First call triggers rate limit error 6
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"error_code": 6,
					"error_msg":  "Too many requests per second",
				},
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": []map[string]interface{}{
				{"id": 123, "name": "Success Group"},
			},
		})
	}))
	defer server.Close()

	client := newVKAPIClient("test_token")
	client.apiURL = server.URL + "/"

	group, err := client.getGroup(context.Background(), "123")
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, int64(123), group.ID)
	require.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestVkVideoBuilder_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"error_code": 5,
				"error_msg":  "User authorization failed: invalid access_token (4).",
			},
		})
	}))
	defer server.Close()

	b, err := NewVkVideoBuilder("bad_token", nil)
	require.NoError(t, err)
	b.client.apiURL = server.URL + "/"

	cfg := &feed.Config{
		URL:      "https://vkvideo.ru/@labelcom",
		PageSize: 10,
	}

	_, err = b.Build(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid access_token")
}
