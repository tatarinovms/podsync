package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockFileSystem struct{}

func (m *mockFileSystem) Open(name string) (http.File, error) {
	return nil, http.ErrMissingFile
}

func TestServerPathPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := fs.NewLocal(tmpDir, false, false)
	require.NoError(t, err)

	_, err = storage.Create(context.Background(), "example.xml", bytes.NewReader([]byte("feed content")))
	require.NoError(t, err)
	_, err = storage.Create(context.Background(), "example/episode.mp3", bytes.NewReader([]byte("audio content")))
	require.NoError(t, err)

	srv := New(Config{Port: 8080, Path: "podcasts"}, storage, nil)

	tests := []struct {
		name   string
		path   string
		status int
		body   string
	}{
		{
			name:   "feed under configured path",
			path:   "/podcasts/example.xml",
			status: http.StatusOK,
			body:   "feed content",
		},
		{
			name:   "episode under configured path",
			path:   "/podcasts/example/episode.mp3",
			status: http.StatusOK,
			body:   "audio content",
		},
		{
			name:   "file outside configured path",
			path:   "/example.xml",
			status: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.status, rec.Code)
			if tt.body != "" {
				assert.Equal(t, tt.body, rec.Body.String())
			}
		})
	}
}

func TestDebugEndpointDisabledByDefault(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Path: "feeds",
	}

	srv := New(cfg, &mockFileSystem{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	// Should return 404 when debug endpoints are disabled
	assert.Equal(t, http.StatusNotFound, rec.Code)
	// Should NOT contain expvar data
	assert.False(t, strings.Contains(rec.Body.String(), "cmdline"))
}

func TestDebugEndpointEnabledWhenConfigured(t *testing.T) {
	cfg := Config{
		Port:           8080,
		Path:           "feeds",
		DebugEndpoints: true,
	}

	srv := New(cfg, &mockFileSystem{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	// Should return 200 and JSON content when debug endpoints are enabled
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	// Verify it contains expvar data (cmdline is always present)
	assert.True(t, strings.Contains(rec.Body.String(), "cmdline"))
}

func TestNoIndexDisabledByDefault(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Path: "feeds",
	}

	srv := New(cfg, &mockFileSystem{}, nil)

	// robots.txt should return 404 when disabled
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// X-Robots-Tag header should not be present on feed requests
	req = httptest.NewRequest(http.MethodGet, "/feeds/test.xml", nil)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Empty(t, rec.Header().Get("X-Robots-Tag"))
}

func TestNoIndexEnabledWhenConfigured(t *testing.T) {
	cfg := Config{
		Port:    8080,
		Path:    "feeds",
		NoIndex: true,
	}

	srv := New(cfg, &mockFileSystem{}, nil)

	// robots.txt should return disallow all
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "User-agent: *")
	assert.Contains(t, rec.Body.String(), "Disallow: /")

	// X-Robots-Tag header should be present on all responses
	req = httptest.NewRequest(http.MethodGet, "/feeds/test.xml", nil)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, "noindex, nofollow", rec.Header().Get("X-Robots-Tag"))
}

func TestNoListingDisabledByDefault(t *testing.T) {
	tmpDir := t.TempDir()

	// Create storage with NoListing disabled (default)
	storage, err := fs.NewLocal(tmpDir, false, false)
	require.NoError(t, err)

	// Create a file inside a subdirectory
	_, err = storage.Create(context.Background(), "feeds/episode.mp3", bytes.NewReader([]byte("audio content")))
	require.NoError(t, err)

	cfg := Config{
		Port: 8080,
		Path: "",
	}

	srv := New(cfg, storage, nil)

	// Accessing a directory should return 200 with directory listing
	req := httptest.NewRequest(http.MethodGet, "/feeds/", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "episode.mp3")

	// Accessing root should also return 200 with directory listing
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "feeds")

	// Accessing a file should work
	req = httptest.NewRequest(http.MethodGet, "/feeds/episode.mp3", nil)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "audio content", rec.Body.String())
}

func TestNoListingEnabledWhenConfigured(t *testing.T) {
	tmpDir := t.TempDir()

	storage, err := fs.NewLocal(tmpDir, false, true)
	require.NoError(t, err)

	// Create a file inside a subdirectory
	_, err = storage.Create(context.Background(), "feeds/episode.mp3", bytes.NewReader([]byte("audio content")))
	require.NoError(t, err)

	cfg := Config{
		Port: 8080,
		Path: "",
	}

	srv := New(cfg, storage, nil)

	// Accessing a directory should return 404
	req := httptest.NewRequest(http.MethodGet, "/feeds/", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Accessing root should also return 404
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Accessing a file should still work
	req = httptest.NewRequest(http.MethodGet, "/feeds/episode.mp3", nil)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "audio content", rec.Body.String())
}

func TestMCPHandlerRouting(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "secretpassword",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)

	mockMCP := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"mcp-ok"}`))
	})
	srv.SetMCPHandler(mockMCP)

	// Unauthenticated request to /mcp should return 401
	reqUnauth := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	recUnauth := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recUnauth, reqUnauth)
	assert.Equal(t, http.StatusUnauthorized, recUnauth.Code)

	// Authenticated request to /mcp should return 200 and invoke handler
	reqAuth := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	reqAuth.SetBasicAuth("admin", "secretpassword")
	recAuth := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recAuth, reqAuth)
	assert.Equal(t, http.StatusOK, recAuth.Code)
	assert.Contains(t, recAuth.Body.String(), "mcp-ok")
}

func TestMCPConfigEndpoint(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:    true,
			Username:   "mycustomadmin",
			Password:   "mysecretpass123",
			ConfigPath: "/custom/path/config.toml",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)

	// Unauthenticated request should return 401
	reqUnauth := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/config", nil)
	recUnauth := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recUnauth, reqUnauth)
	assert.Equal(t, http.StatusUnauthorized, recUnauth.Code)

	// Authenticated request should return 200 with credentials
	reqAuth := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/config", nil)
	reqAuth.SetBasicAuth("mycustomadmin", "mysecretpass123")
	recAuth := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recAuth, reqAuth)
	assert.Equal(t, http.StatusOK, recAuth.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(recAuth.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "mycustomadmin", resp["username"])
	assert.Equal(t, "mysecretpass123", resp["password"])
	assert.Contains(t, resp["basic_auth_header"], "Basic ")
	assert.Contains(t, resp["config_path"], "config.toml")
}

type mockDownloaderAdminMgr struct {
	dlCfg          DownloaderConfigInfo
	cookies        string
	testResponse   TestDownloaderResult
	lastProvider   string
	lastTokens     []string
	lastDeleteIdx  int
	wasAppend      bool
	lastFeedID     string
	lastFeedUpdate *FeedConfigUpdate
}

func (m *mockDownloaderAdminMgr) ListFeeds(ctx context.Context) ([]FeedSummary, error) {
	return nil, nil
}
func (m *mockDownloaderAdminMgr) GetFeedsStatus(ctx context.Context) ([]FeedStatusItem, error) {
	return []FeedStatusItem{{ID: "test", Status: FeedStatusIdle}}, nil
}
func (m *mockDownloaderAdminMgr) GetFeedDetail(ctx context.Context, id string) (*FeedSummary, []*model.Episode, error) {
	return nil, nil, nil
}
func (m *mockDownloaderAdminMgr) AddFeed(ctx context.Context, cfg *feed.Config) error { return nil }
func (m *mockDownloaderAdminMgr) UpdateFeed(ctx context.Context, id string, update *FeedConfigUpdate) error {
	m.lastFeedID = id
	m.lastFeedUpdate = update
	return nil
}
func (m *mockDownloaderAdminMgr) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	return nil
}
func (m *mockDownloaderAdminMgr) TriggerUpdate(ctx context.Context, id string) error { return nil }
func (m *mockDownloaderAdminMgr) TriggerUpdateAll(ctx context.Context) ([]string, error) {
	return []string{"test"}, nil
}
func (m *mockDownloaderAdminMgr) RetryEpisode(ctx context.Context, feedID, episodeID string) error {
	return nil
}
func (m *mockDownloaderAdminMgr) GetTokens(ctx context.Context) []TokenInfo { return nil }
func (m *mockDownloaderAdminMgr) UpdateTokens(ctx context.Context, provider string, tokens []string) error {
	m.lastProvider = provider
	m.lastTokens = tokens
	m.wasAppend = false
	return nil
}
func (m *mockDownloaderAdminMgr) AddTokens(ctx context.Context, provider string, tokens []string) error {
	m.lastProvider = provider
	m.lastTokens = tokens
	m.wasAppend = true
	return nil
}
func (m *mockDownloaderAdminMgr) DeleteToken(ctx context.Context, provider string, index int) error {
	m.lastProvider = provider
	m.lastDeleteIdx = index
	return nil
}
func (m *mockDownloaderAdminMgr) GetSystemStats(ctx context.Context) (*SystemStats, error) {
	return nil, nil
}

func (m *mockDownloaderAdminMgr) GetDownloaderConfig(ctx context.Context) (*DownloaderConfigInfo, error) {
	return &m.dlCfg, nil
}

func (m *mockDownloaderAdminMgr) UpdateDownloaderConfig(ctx context.Context, cfg *DownloaderConfigUpdate) error {
	if cfg.Timeout != nil {
		m.dlCfg.Timeout = *cfg.Timeout
	}
	if cfg.Proxy != nil {
		m.dlCfg.Proxy = *cfg.Proxy
	}
	if cfg.Language != nil {
		m.dlCfg.Language = *cfg.Language
	}
	return nil
}

func (m *mockDownloaderAdminMgr) GetCookiesContent(ctx context.Context) (string, error) {
	return m.cookies, nil
}

func (m *mockDownloaderAdminMgr) UpdateCookiesContent(ctx context.Context, content string) error {
	m.cookies = content
	return nil
}

func (m *mockDownloaderAdminMgr) TestDownloader(ctx context.Context, testURL string) (*TestDownloaderResult, error) {
	return &m.testResponse, nil
}

func (m *mockDownloaderAdminMgr) GetDownloadErrors(ctx context.Context, feedID string, limit int) ([]DownloadErrorItem, error) {
	return []DownloadErrorItem{}, nil
}

func (m *mockDownloaderAdminMgr) GetRecentDownloads(ctx context.Context, feedID string, limit int) ([]RecentDownloadItem, error) {
	return []RecentDownloadItem{}, nil
}

func (m *mockDownloaderAdminMgr) GetSyncStatus(ctx context.Context) (*SyncStatusInfo, error) {
	return &SyncStatusInfo{}, nil
}

func (m *mockDownloaderAdminMgr) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	return nil
}

func (m *mockDownloaderAdminMgr) GetSystemLogs(ctx context.Context, filter LogFilter) (*LogResult, error) {
	return &LogResult{
		Entries: []LogEntry{
			{ID: 1, Level: "info", Message: "test log"},
		},
		Total:        1,
		CurrentLevel: "info",
	}, nil
}

func (m *mockDownloaderAdminMgr) ClearSystemLogs(ctx context.Context) error {
	return nil
}

func (m *mockDownloaderAdminMgr) SetLogLevel(ctx context.Context, level string) (string, error) {
	return level, nil
}

func (m *mockDownloaderAdminMgr) GetLogLevel(ctx context.Context) string {
	return "info"
}

func (m *mockDownloaderAdminMgr) DownloadLogs(ctx context.Context) ([]byte, string, error) {
	return []byte("test log content\n"), "podsync.log", nil
}

func (m *mockDownloaderAdminMgr) ListAudiobookDirs(ctx context.Context) ([]AudiobookDirInfo, error) {
	return nil, nil
}

func (m *mockDownloaderAdminMgr) ImportAudiobook(ctx context.Context, req *ImportAudiobookRequest) (*feed.Config, error) {
	return &feed.Config{ID: req.ID}, nil
}

func (m *mockDownloaderAdminMgr) GetAudiobooksDir() string {
	return "./audiobooks"
}

func (m *mockDownloaderAdminMgr) SaveAudiobookFile(bookDir string, filename string, r io.Reader) error {
	return nil
}

func (m *mockDownloaderAdminMgr) DeleteAudiobookDir(ctx context.Context, dirName string) error {
	return nil
}

func TestDownloaderEndpoints(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{
		dlCfg: DownloaderConfigInfo{
			Timeout:       15,
			Proxy:         "http://proxy:8080",
			FFmpegVersion: "7.1",
		},
		cookies: "# Netscape HTTP Cookie File",
		testResponse: TestDownloaderResult{
			Success: true,
			Title:   "Sample Video",
			Channel: "Sample Channel",
		},
	}
	srv.SetAdminManager(mgr)

	// 1. GET /api/v1/downloader
	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloader", nil)
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "http://proxy:8080")
	assert.Contains(t, rec.Body.String(), "7.1")

	// 2. POST /api/v1/downloader
	updateJSON := `{"timeout": 25, "proxy": "http://newproxy:8080", "language": "ru"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/downloader", strings.NewReader(updateJSON))
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 25, mgr.dlCfg.Timeout)
	assert.Equal(t, "http://newproxy:8080", mgr.dlCfg.Proxy)
	assert.Equal(t, "ru", mgr.dlCfg.Language)

	// 3. GET /api/v1/downloader/cookies
	req = httptest.NewRequest(http.MethodGet, "/api/v1/downloader/cookies", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Netscape HTTP Cookie File")

	// 4. POST /api/v1/downloader/cookies
	cookiesJSON := `{"content": "# Updated cookies"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/downloader/cookies", strings.NewReader(cookiesJSON))
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "# Updated cookies", mgr.cookies)

	// 5. POST /api/v1/downloader/test
	testJSON := `{"url": "https://youtube.com/watch?v=123"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/downloader/test", strings.NewReader(testJSON))
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Sample Video")
}

func TestAdminTokensAPI(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{}
	srv.SetAdminManager(mgr)

	// 1. POST /api/v1/tokens (replace)
	body := `{"provider":"youtube","tokens":["k1","k2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", strings.NewReader(body))
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "youtube", mgr.lastProvider)
	assert.Equal(t, []string{"k1", "k2"}, mgr.lastTokens)
	assert.False(t, mgr.wasAppend)

	// 2. POST /api/v1/tokens (append)
	body = `{"provider":"youtube","tokens":["k3"],"append":true}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tokens", strings.NewReader(body))
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"k3"}, mgr.lastTokens)
	assert.True(t, mgr.wasAppend)

	// 3. DELETE /api/v1/tokens with index
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/tokens?provider=youtube&index=1", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, mgr.lastDeleteIdx)

	// 4. DELETE /api/v1/tokens without index (all)
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/tokens?provider=youtube", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, -1, mgr.lastDeleteIdx)
}

func TestUpdateFeedAPI(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{}
	srv.SetAdminManager(mgr)

	body := `{"page_size": 25, "quality": "low", "keep_last": 5, "cron_schedule": "@every 6h", "language": "ru"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/feeds/FEED123", strings.NewReader(body))
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "FEED123", mgr.lastFeedID)
	require.NotNil(t, mgr.lastFeedUpdate)
	assert.Equal(t, 25, mgr.lastFeedUpdate.PageSize)
	assert.Equal(t, "low", mgr.lastFeedUpdate.Quality)
	assert.Equal(t, 5, mgr.lastFeedUpdate.KeepLast)
	assert.Equal(t, "@every 6h", mgr.lastFeedUpdate.CronSchedule)
	assert.Equal(t, "ru", mgr.lastFeedUpdate.Language)
}

func TestTriggerUpdateAllAPI(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{}
	srv.SetAdminManager(mgr)

	// 1. POST /api/v1/feeds/update
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feeds/update", nil)
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":true`)
	assert.Contains(t, rec.Body.String(), `"count":1`)
	assert.Contains(t, rec.Body.String(), `"feeds":["test"]`)

	// 2. POST /api/v1/feeds/update-all
	req = httptest.NewRequest(http.MethodPost, "/api/v1/feeds/update-all", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":true`)

	// 3. GET /api/v1/feeds/update should be MethodNotAllowed
	req = httptest.NewRequest(http.MethodGet, "/api/v1/feeds/update", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestFeedsStatusEndpoint(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}

	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{}
	srv.SetAdminManager(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feeds/status", nil)
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"test"`)
	assert.Contains(t, rec.Body.String(), `"status":"idle"`)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/feeds/status", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestSystemLogsEndpoints(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{}
	srv.SetAdminManager(mgr)

	// 1. GET /api/v1/system/logs
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/logs?limit=50", nil)
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "test log")

	// 2. DELETE /api/v1/system/logs
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/system/logs", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "cleared")

	// 3. GET /api/v1/system/logs/level
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/logs/level", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"level":"info"`)

	// 4. POST /api/v1/system/logs/level
	req = httptest.NewRequest(http.MethodPost, "/api/v1/system/logs/level", strings.NewReader(`{"level":"debug"}`))
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"level":"debug"`)

	// 5. GET /api/v1/system/logs/download
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/logs/download", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "test log content")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "podsync.log")
}

func TestAudiobookEndpoints(t *testing.T) {
	cfg := Config{
		Port: 8080,
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "password",
		},
	}
	srv := New(cfg, &mockFileSystem{}, nil)
	mgr := &mockDownloaderAdminMgr{}
	srv.SetAdminManager(mgr)

	// 1. GET /api/v1/audiobooks/dirs
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audiobooks/dirs", nil)
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "[]", rec.Body.String())

	// 2. GET /api/v1/audiobooks/config
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audiobooks/config", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "audiobooks_dir")

	// 3. POST /api/v1/audiobooks/import
	importJSON := `{"dir_name": "Orwell - 1984", "id": "1984", "title": "1984"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/audiobooks/import", strings.NewReader(importJSON))
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"feed_id":"1984"`)

	// 4. DELETE /api/v1/audiobooks/dirs
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/audiobooks/dirs?name=Orwell+-+1984", nil)
	req.SetBasicAuth("admin", "password")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"success":true`)
}

func TestAudiobookUploadTooLarge(t *testing.T) {
	srv := New(Config{Admin: AdminConfig{Enabled: true, Username: "admin", Password: "password"}}, &mockFileSystem{}, nil)
	srv.SetAdminManager(&mockDownloaderAdminMgr{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audiobooks/upload", strings.NewReader("x"))
	req.ContentLength = (1024 << 20) + 1
	req.SetBasicAuth("admin", "password")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestAdminAssets(t *testing.T) {
	srv := New(Config{Admin: AdminConfig{Enabled: true, Username: "admin", Password: "password"}}, &mockFileSystem{}, nil)
	for _, path := range []string{"/admin/", "/admin/admin.css", "/admin/theme.js", "/admin/admin.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetBasicAuth("admin", "password")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.NotEmpty(t, rec.Body.String(), path)
	}
}
