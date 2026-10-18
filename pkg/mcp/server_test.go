package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
)

type mockAdminManager struct {
	feeds       []web.FeedSummary
	stats       *web.SystemStats
	tokens      []web.TokenInfo
	addFeedErr  error
	updFeedErr  error
	retryErr    error
	delFeedErr  error
	updTokenErr error
}

func (m *mockAdminManager) ListFeeds(ctx context.Context) ([]web.FeedSummary, error) {
	return m.feeds, nil
}

func (m *mockAdminManager) GetFeedsStatus(ctx context.Context) ([]web.FeedStatusItem, error) {
	result := make([]web.FeedStatusItem, 0, len(m.feeds))
	for _, f := range m.feeds {
		status := web.FeedStatusIdle
		if f.Updating {
			status = web.FeedStatusRunning
		}
		result = append(result, web.FeedStatusItem{ID: f.ID, Status: status})
	}
	return result, nil
}

func (m *mockAdminManager) GetFeedDetail(ctx context.Context, id string) (*web.FeedSummary, []*model.Episode, error) {
	for _, f := range m.feeds {
		if f.ID == id {
			return &f, []*model.Episode{
				{ID: "ep1", Title: "Episode 1", Status: model.EpisodeDownloaded},
			}, nil
		}
	}
	return nil, nil, nil
}

func (m *mockAdminManager) AddFeed(ctx context.Context, cfg *feed.Config) error {
	if m.addFeedErr != nil {
		return m.addFeedErr
	}
	cfg.ID = "NEW_FEED"
	m.feeds = append(m.feeds, web.FeedSummary{ID: cfg.ID, URL: cfg.URL})
	return nil
}

func (m *mockAdminManager) UpdateFeed(ctx context.Context, id string, update *web.FeedConfigUpdate) error {
	return m.updFeedErr
}

func (m *mockAdminManager) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	return m.delFeedErr
}

func (m *mockAdminManager) TriggerUpdate(ctx context.Context, id string) error {
	return m.updFeedErr
}

func (m *mockAdminManager) TriggerUpdateAll(ctx context.Context) ([]string, error) {
	if m.updFeedErr != nil {
		return nil, m.updFeedErr
	}
	var ids []string
	for _, f := range m.feeds {
		ids = append(ids, f.ID)
	}
	return ids, nil
}

func (m *mockAdminManager) RetryEpisode(ctx context.Context, feedID, episodeID string) error {
	return m.retryErr
}

func (m *mockAdminManager) GetTokens(ctx context.Context) []web.TokenInfo {
	return m.tokens
}

func (m *mockAdminManager) UpdateTokens(ctx context.Context, provider string, tokens []string) error {
	return m.updTokenErr
}

func (m *mockAdminManager) AddTokens(ctx context.Context, provider string, tokens []string) error {
	return m.updTokenErr
}

func (m *mockAdminManager) DeleteToken(ctx context.Context, provider string, index int) error {
	return m.updTokenErr
}

func (m *mockAdminManager) GetSystemStats(ctx context.Context) (*web.SystemStats, error) {
	return m.stats, nil
}

func (m *mockAdminManager) GetDownloaderConfig(ctx context.Context) (*web.DownloaderConfigInfo, error) {
	return &web.DownloaderConfigInfo{
		Timeout:        10,
		HasCookiesFile: false,
	}, nil
}

func (m *mockAdminManager) UpdateDownloaderConfig(ctx context.Context, cfg *web.DownloaderConfigUpdate) error {
	return nil
}

func (m *mockAdminManager) GetCookiesContent(ctx context.Context) (string, error) {
	return "", nil
}

func (m *mockAdminManager) UpdateCookiesContent(ctx context.Context, content string) error {
	return nil
}

func (m *mockAdminManager) TestDownloader(ctx context.Context, testURL string) (*web.TestDownloaderResult, error) {
	return &web.TestDownloaderResult{
		Success: true,
		Title:   "Test Title",
		Channel: "Test Channel",
	}, nil
}

func (m *mockAdminManager) GetDownloadErrors(ctx context.Context, feedID string, limit int) ([]web.DownloadErrorItem, error) {
	return []web.DownloadErrorItem{
		{FeedID: "TEST", EpisodeID: "ep1", EpisodeTitle: "Failed Ep", Error: "Sign in to confirm you are not a bot", Recommendation: "Configure cookies"},
	}, nil
}

func (m *mockAdminManager) GetRecentDownloads(ctx context.Context, feedID string, limit int) ([]web.RecentDownloadItem, error) {
	return []web.RecentDownloadItem{
		{FeedID: "TEST", EpisodeID: "ep2", EpisodeTitle: "Downloaded Ep", MediaURL: "http://localhost:8080/TEST/ep2.mp3"},
	}, nil
}

func (m *mockAdminManager) GetSyncStatus(ctx context.Context) (*web.SyncStatusInfo, error) {
	return &web.SyncStatusInfo{
		ActiveFeeds:        1,
		DownloadedEpisodes: 5,
		FailedEpisodes:     1,
	}, nil
}

func (m *mockAdminManager) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	return nil
}

func (m *mockAdminManager) GetSystemLogs(ctx context.Context, filter web.LogFilter) (*web.LogResult, error) {
	return &web.LogResult{
		Entries: []web.LogEntry{
			{ID: 1, Level: "info", Message: "mock log entry"},
		},
		Total:        1,
		CurrentLevel: "info",
	}, nil
}

func (m *mockAdminManager) ClearSystemLogs(ctx context.Context) error {
	return nil
}

func (m *mockAdminManager) SetLogLevel(ctx context.Context, level string) (string, error) {
	return level, nil
}

func (m *mockAdminManager) GetLogLevel(ctx context.Context) string {
	return "info"
}

func (m *mockAdminManager) DownloadLogs(ctx context.Context) ([]byte, string, error) {
	return []byte("mock log content\n"), "podsync.log", nil
}

func (m *mockAdminManager) ListAudiobookDirs(ctx context.Context) ([]web.AudiobookDirInfo, error) {
	return []web.AudiobookDirInfo{
		{
			Name:            "Orwell - 1984",
			Path:            "/data/audiobooks/Orwell - 1984",
			FileCount:       12,
			HasCover:        true,
			TotalSize:       1024000,
			SuggestedTitle:  "1984",
			SuggestedAuthor: "Orwell",
		},
	}, nil
}

func (m *mockAdminManager) ImportAudiobook(ctx context.Context, req *web.ImportAudiobookRequest) (*feed.Config, error) {
	return &feed.Config{
		ID:           req.ID,
		URL:          "audiobook://" + req.DirName,
		Format:       model.FormatCustom,
		PlaylistSort: model.SortingAsc,
	}, nil
}

func (m *mockAdminManager) GetAudiobooksDir() string {
	return "/data/audiobooks"
}

func (m *mockAdminManager) SaveAudiobookFile(bookDir string, filename string, r io.Reader) error {
	return nil
}

func (m *mockAdminManager) DeleteAudiobookDir(ctx context.Context, dirName string) error {
	return nil
}

func setupTestServer() (*Server, *mockAdminManager) {
	mockMgr := &mockAdminManager{
		feeds: []web.FeedSummary{
			{ID: "TEST", Title: "Test Feed", URL: "https://youtube.com/channel/test"},
		},
		stats: &web.SystemStats{
			Version:   "v2.13.9",
			HasYtDlp:  true,
			HasFFmpeg: true,
			MemoryMB:  42.5,
		},
		tokens: []web.TokenInfo{
			{Provider: "youtube", Tokens: []string{"AIza••••9Y4"}, FromEnv: false},
		},
	}
	return NewServer(mockMgr), mockMgr
}

func TestInitialize(t *testing.T) {
	server, _ := setupTestServer()
	ctx := context.Background()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`
	respBytes, err := server.HandleMessage(ctx, []byte(req))
	require.NoError(t, err)

	var resp JSONRPCResponse
	err = json.Unmarshal(respBytes, &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(1), resp.ID)
	assert.Nil(t, resp.Error)

	var initResult InitializeResult
	resData, _ := json.Marshal(resp.Result)
	err = json.Unmarshal(resData, &initResult)
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, initResult.ProtocolVersion)
	assert.Equal(t, "podsync-mcp", initResult.ServerInfo.Name)
	assert.NotNil(t, initResult.Capabilities.Tools)
	assert.NotNil(t, initResult.Capabilities.Resources)
}

func TestToolsList(t *testing.T) {
	server, _ := setupTestServer()
	ctx := context.Background()

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	respBytes, err := server.HandleMessage(ctx, []byte(req))
	require.NoError(t, err)

	var resp JSONRPCResponse
	err = json.Unmarshal(respBytes, &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)

	var listResult ListToolsResult
	resData, _ := json.Marshal(resp.Result)
	err = json.Unmarshal(resData, &listResult)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResult.Tools), 7)

	names := make(map[string]bool)
	for _, tool := range listResult.Tools {
		names[tool.Name] = true
	}
	assert.True(t, names["list_feeds"])
	assert.True(t, names["get_feed"])
	assert.True(t, names["add_feed"])
	assert.True(t, names["delete_feed"])
	assert.True(t, names["get_system_stats"])
	assert.True(t, names["get_tokens"])
}

func TestToolsCall(t *testing.T) {
	server, _ := setupTestServer()
	ctx := context.Background()

	t.Run("list_feeds", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_feeds","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		require.Len(t, callResult.Content, 1)
		assert.Contains(t, callResult.Content[0].Text, "TEST")
	})

	t.Run("add_feed", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"add_feed","arguments":{"url":"https://youtube.com/channel/new","format":"audio"}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Successfully added feed")
	})

	t.Run("update_all_feeds", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"update_all_feeds","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		require.Len(t, callResult.Content, 1)
		assert.Contains(t, callResult.Content[0].Text, "Update triggered for 2 feed(s)")
	})

	t.Run("get_system_stats", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_system_stats","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)
		resData, _ := json.Marshal(resp.Result)
		assert.Contains(t, string(resData), "v2.13.9")
	})

	t.Run("get_downloader_config", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":60,"method":"tools/call","params":{"name":"get_downloader_config","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)
		resData, _ := json.Marshal(resp.Result)
		assert.Contains(t, string(resData), "timeout")
	})

	t.Run("update_downloader_config", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":61,"method":"tools/call","params":{"name":"update_downloader_config","arguments":{"timeout":25,"language":"ru"}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)
		resData, _ := json.Marshal(resp.Result)
		assert.Contains(t, string(resData), "Successfully updated downloader configuration")
	})

	t.Run("get_download_errors", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_download_errors","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Sign in to confirm you are not a bot")
		assert.Contains(t, callResult.Content[0].Text, "Configure cookies")
	})

	t.Run("get_recent_downloads", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_recent_downloads","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Downloaded Ep")
	})

	t.Run("get_sync_status", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_sync_status","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "downloaded_episodes")
	})

	t.Run("delete_tokens_all", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"delete_tokens","arguments":{"provider":"youtube"}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Successfully deleted all API tokens")
	})

	t.Run("delete_tokens_single_index", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"delete_tokens","arguments":{"provider":"youtube","index":1}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Successfully deleted token #1")
	})

	t.Run("get_system_logs", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"get_system_logs","arguments":{"level":"info","limit":20}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "mock log entry")
	})

	t.Run("list_audiobook_dirs", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"list_audiobook_dirs","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Orwell - 1984")
	})

	t.Run("import_audiobook", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"import_audiobook","arguments":{"dir_name":"Orwell - 1984","feed_id":"1984","title":"Nineteen Eighty-Four"}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)

		var callResult CallToolResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &callResult)
		assert.False(t, callResult.IsError)
		assert.Contains(t, callResult.Content[0].Text, "Successfully imported audiobook as feed")
	})
}

func TestResources(t *testing.T) {
	server, _ := setupTestServer()
	ctx := context.Background()

	t.Run("list", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":6,"method":"resources/list"}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		_ = json.Unmarshal(respBytes, &resp)
		assert.Nil(t, resp.Error)

		var res ListResourcesResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &res)
		assert.Len(t, res.Resources, 3)
	})

	t.Run("read", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":7,"method":"resources/read","params":{"uri":"podsync://feeds"}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		_ = json.Unmarshal(respBytes, &resp)
		assert.Nil(t, resp.Error)

		var res ReadResourceResult
		resData, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(resData, &res)
		require.Len(t, res.Contents, 1)
		assert.Contains(t, res.Contents[0].Text, "TEST")
	})
}

func TestHTTPHandler(t *testing.T) {
	server, _ := setupTestServer()
	handler := NewHTTPHandler(server)

	t.Run("direct json-rpc POST", func(t *testing.T) {
		reqBody := `{"jsonrpc":"2.0","id":10,"method":"ping"}`
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"id":10`)
	})

	t.Run("status endpoint GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "podsync-mcp")
	})
}

func TestRunStdio(t *testing.T) {
	server, _ := setupTestServer()
	ctx := context.Background()

	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"

	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	err := RunStdio(ctx, server, in, out)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], `"id":1`)
}

func TestMCPToolsDocumentationSync(t *testing.T) {
	server, _ := setupTestServer()
	tools := server.getToolDefinitions()
	require.NotEmpty(t, tools)

	// 1. Verify services/web/ui/index.html
	uiPath := filepath.Join("..", "..", "services", "web", "ui", "index.html")
	uiBytes, err := os.ReadFile(uiPath)
	require.NoError(t, err, "failed to read services/web/ui/index.html")
	uiContent := string(uiBytes)

	// 2. Verify html/index.html
	htmlPath := filepath.Join("..", "..", "html", "index.html")
	htmlBytes, err := os.ReadFile(htmlPath)
	require.NoError(t, err, "failed to read html/index.html")
	assert.Equal(t, string(uiBytes), string(htmlBytes), "services/web/ui/index.html and html/index.html must be byte-for-byte identical")

	// 3. Verify docs/mcp.md
	docsPath := filepath.Join("..", "..", "docs", "mcp.md")
	docsBytes, err := os.ReadFile(docsPath)
	require.NoError(t, err, "failed to read docs/mcp.md")
	docsContent := string(docsBytes)

	// 4. Verify AGENTS.md
	agentsPath := filepath.Join("..", "..", "AGENTS.md")
	agentsBytes, err := os.ReadFile(agentsPath)
	require.NoError(t, err, "failed to read AGENTS.md")
	agentsContent := string(agentsBytes)

	for _, tool := range tools {
		expectedHTMLSnippet := fmt.Sprintf("<td><code>%s</code></td>", tool.Name)
		assert.Contains(t, uiContent, expectedHTMLSnippet, "Tool %q is registered in MCP server but missing from services/web/ui/index.html", tool.Name)

		expectedDocsSnippet := fmt.Sprintf("`%s`", tool.Name)
		assert.Contains(t, docsContent, expectedDocsSnippet, "Tool %q is registered in MCP server but missing from docs/mcp.md", tool.Name)
		assert.Contains(t, agentsContent, expectedDocsSnippet, "Tool %q is registered in MCP server but missing from AGENTS.md", tool.Name)
	}

	assert.Contains(t, docsContent, fmt.Sprintf("%d specialized tools", len(tools)), "docs/mcp.md tool count is out of date")
	assert.Contains(t, agentsContent, fmt.Sprintf("%d MCP tools", len(tools)), "AGENTS.md tool count is out of date")
}
