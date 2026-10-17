package builder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

func TestRutubeBuilder_DateParsing(t *testing.T) {
	d1, err := parseRutubeDate("2026-09-17T21:44:53")
	require.NoError(t, err)
	require.Equal(t, 2026, d1.Year())
	require.Equal(t, time.September, d1.Month())
	require.Equal(t, 17, d1.Day())

	d2, err := parseRutubeDate("2026-08-01T08:00:40+03:00")
	require.NoError(t, err)
	require.Equal(t, 2026, d2.Year())

	d3, err := parseRutubeDate("2026-05-10")
	require.NoError(t, err)
	require.Equal(t, 2026, d3.Year())

	_, err = parseRutubeDate("")
	require.Error(t, err)

	_, err = parseRutubeDate("invalid-date")
	require.Error(t, err)
}

func TestRutubeBuilder_ChannelFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, rutubeDefaultUserAgent, r.Header.Get("User-Agent"))

		if r.URL.Path == "/video/person/25758171/" {
			switch r.URL.Query().Get("page") {
			case "1":
				resp := rutubePaginatedResponse{
					HasNext: true,
					Next:    "/video/person/25758171/?page=2",
					Page:    1,
					Results: []*rutubeVideoItem{
						{
							ID:            "vid1",
							Title:         "Episode 1",
							Description:   "Description 1",
							ThumbnailURL:  "https://pic.rtbcdn.ru/thumb1.jpg",
							VideoURL:      "https://rutube.ru/video/vid1/",
							Duration:      600,
							PublicationTs: "2026-09-17T21:44:53",
							Author: rutubeAuthor{
								ID:        25758171,
								Name:      "Channel Author",
								AvatarURL: "https://pic.rtbcdn.ru/avatar.jpg",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			case "2":
				resp := rutubePaginatedResponse{
					HasNext: false,
					Page:    2,
					Results: []*rutubeVideoItem{
						{
							ID:            "vid2",
							Title:         "Episode 2",
							Description:   "Description 2",
							ThumbnailURL:  "https://pic.rtbcdn.ru/thumb2.jpg",
							VideoURL:      "https://rutube.ru/video/vid2/",
							Duration:      1200,
							PublicationTs: "2026-09-10T10:00:00",
							Author: rutubeAuthor{
								ID:   25758171,
								Name: "Channel Author",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL:      "https://rutube.ru/channel/25758171/",
		PageSize: 10,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "Channel Author", result.Title)
	require.Equal(t, "Channel Author", result.Author)
	require.Equal(t, "https://pic.rtbcdn.ru/avatar.jpg", result.CoverArt)
	require.Equal(t, "https://rutube.ru/channel/25758171/", result.ItemURL)
	require.Len(t, result.Episodes, 2)

	require.Equal(t, "vid1", result.Episodes[0].ID)
	require.Equal(t, "Episode 1", result.Episodes[0].Title)
	require.Equal(t, int64(600), result.Episodes[0].Duration)

	require.Equal(t, "vid2", result.Episodes[1].ID)
	require.Equal(t, "Episode 2", result.Episodes[1].Title)
	require.Equal(t, int64(1200), result.Episodes[1].Duration)
}

func TestRutubeBuilder_UserFeedSlug(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/u/mychannel/" {
			html := `<!DOCTYPE html><html><head><script>window.reduxState={"api":{"channel_id":998877}};</script></head><body></body></html>`
			_, _ = w.Write([]byte(html))
			return
		}

		if r.URL.Path == "/video/person/998877/" {
			resp := rutubePaginatedResponse{
				HasNext: false,
				Page:    1,
				Results: []*rutubeVideoItem{
					{
						ID:            "slugvid",
						Title:         "Slug Video",
						PublicationTs: "2026-09-01T12:00:00",
						Author: rutubeAuthor{
							Name: "Slug Author",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.webBaseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL:      "https://rutube.ru/u/mychannel/",
		PageSize: 10,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Slug Author", result.Title)
	require.Len(t, result.Episodes, 1)
	require.Equal(t, "slugvid", result.Episodes[0].ID)
}

func TestRutubeBuilder_PlaylistFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist/custom/1444174/" {
			meta := rutubePlaylistMeta{
				ID:           1444174,
				Title:        "Custom Playlist Title",
				Description:  "Playlist Desc",
				ThumbnailURL: "https://pic.rtbcdn.ru/plthumb.jpg",
			}
			_ = json.NewEncoder(w).Encode(meta)
			return
		}

		if r.URL.Path == "/playlist/custom/1444174/videos/" {
			resp := rutubePaginatedResponse{
				HasNext: false,
				Page:    1,
				Results: []*rutubeVideoItem{
					{
						ID:            "plvid1",
						Title:         "Playlist Video 1",
						Description:   "Desc 1",
						ThumbnailURL:  "https://pic.rtbcdn.ru/vt1.jpg",
						Duration:      300,
						PublicationTs: "2026-08-01T08:00:40+03:00",
						Author: rutubeAuthor{
							Name: "Playlist Author",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL:      "https://rutube.ru/plst/1444174/",
		PageSize: 10,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Custom Playlist Title", result.Title)
	require.Equal(t, "Playlist Desc", result.Description)
	require.Equal(t, "Playlist Author", result.Author)
	require.Equal(t, "https://pic.rtbcdn.ru/plthumb.jpg", result.CoverArt)
	require.Len(t, result.Episodes, 1)
	require.Equal(t, "plvid1", result.Episodes[0].ID)
}

func TestRutubeBuilder_VideoFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/video/6459e556582749f8fb4c697b9b90cb8a/" {
			item := rutubeVideoItem{
				ID:            "6459e556582749f8fb4c697b9b90cb8a",
				Title:         "Single Video Title",
				Description:   "Single Video Description",
				ThumbnailURL:  "https://pic.rtbcdn.ru/single.jpg",
				VideoURL:      "https://rutube.ru/video/6459e556582749f8fb4c697b9b90cb8a/",
				Duration:      502,
				PublicationTs: "2026-08-01T08:00:40",
				Author: rutubeAuthor{
					Name: "Single Author",
				},
			}
			_ = json.NewEncoder(w).Encode(item)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL: "https://rutube.ru/video/6459e556582749f8fb4c697b9b90cb8a/?r=plwd",
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Single Video Title", result.Title)
	require.Equal(t, "Single Video Description", result.Description)
	require.Equal(t, "Single Author", result.Author)
	require.Equal(t, "https://pic.rtbcdn.ru/single.jpg", result.CoverArt)
	require.Len(t, result.Episodes, 1)
	require.Equal(t, "6459e556582749f8fb4c697b9b90cb8a", result.Episodes[0].ID)
	require.Equal(t, int64(502), result.Episodes[0].Duration)
}

func TestRutubeBuilder_SortingAsc(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rutubePaginatedResponse{
			HasNext: false,
			Page:    1,
			Results: []*rutubeVideoItem{
				{
					ID:            "vid_new",
					Title:         "New",
					PublicationTs: "2026-09-20T00:00:00",
					Author:        rutubeAuthor{Name: "Author"},
				},
				{
					ID:            "vid_old",
					Title:         "Old",
					PublicationTs: "2026-01-01T00:00:00",
					Author:        rutubeAuthor{Name: "Author"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL:          "https://rutube.ru/channel/12345/",
		PlaylistSort: model.SortingAsc,
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, result.Episodes, 2)
	require.Equal(t, "vid_old", result.Episodes[0].ID)
	require.Equal(t, "vid_new", result.Episodes[1].ID)
}

func TestRutubeBuilder_CustomOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := rutubeVideoItem{
			ID:            "vid123",
			Title:         "Original Title",
			Description:   "Original Desc",
			ThumbnailURL:  "https://pic.rtbcdn.ru/orig.jpg",
			PublicationTs: "2026-08-01T08:00:40",
			Author:        rutubeAuthor{Name: "Original Author"},
		}
		_ = json.NewEncoder(w).Encode(item)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL: "https://rutube.ru/video/vid123/",
		Custom: feed.Custom{
			Title:       "Custom Title",
			Description: "Custom Desc",
			Author:      "Custom Author",
			CoverArt:    "https://example.com/custom.jpg",
		},
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, "Custom Title", result.Title)
	require.Equal(t, "Custom Desc", result.Description)
	require.Equal(t, "Custom Author", result.Author)
	require.Equal(t, "https://example.com/custom.jpg", result.CoverArt)
}

func TestRutubeBuilder_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)
	b.baseURL = server.URL
	b.client = server.Client()

	cfg := &feed.Config{
		URL: "https://rutube.ru/video/vid_err/",
	}

	_, err = b.Build(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}

func TestRutubeBuilder_LiveRealURLs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	b, err := NewRutubeBuilder(nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Channel
	fChan, err := b.Build(ctx, &feed.Config{
		URL:      "https://rutube.ru/channel/25758171/",
		PageSize: 5,
	})
	if err == nil {
		require.Equal(t, model.ProviderRutube, fChan.Provider)
		require.NotEmpty(t, fChan.Title)
		require.NotEmpty(t, fChan.Episodes)
		t.Logf("Live Channel: %s (%d episodes)", fChan.Title, len(fChan.Episodes))
	}

	// 2. Playlist
	fPlst, err := b.Build(ctx, &feed.Config{
		URL:      "https://rutube.ru/plst/1444174/",
		PageSize: 5,
	})
	if err == nil {
		require.Equal(t, model.ProviderRutube, fPlst.Provider)
		require.NotEmpty(t, fPlst.Title)
		require.NotEmpty(t, fPlst.Episodes)
		t.Logf("Live Playlist: %s (%d episodes)", fPlst.Title, len(fPlst.Episodes))
	}

	// 3. Video
	fVid, err := b.Build(ctx, &feed.Config{
		URL: "https://rutube.ru/video/6459e556582749f8fb4c697b9b90cb8a/?r=plwd",
	})
	if err == nil {
		require.Equal(t, model.ProviderRutube, fVid.Provider)
		require.NotEmpty(t, fVid.Title)
		require.Len(t, fVid.Episodes, 1)
		t.Logf("Live Video: %s (%s)", fVid.Title, fVid.Episodes[0].ID)
	}
}
