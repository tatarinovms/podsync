package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func (m *mockAdminManager) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	return m.delFeedErr
}

func (m *mockAdminManager) TriggerUpdate(ctx context.Context, id string) error {
	return m.updFeedErr
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

func (m *mockAdminManager) GetSystemStats(ctx context.Context) (*web.SystemStats, error) {
	return m.stats, nil
}

func (m *mockAdminManager) GetDownloaderConfig(ctx context.Context) (*web.DownloaderConfigInfo, error) {
	return &web.DownloaderConfigInfo{
		Timeout: 10,
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
		Title: "Test Title",
		Channel: "Test Channel",
	}, nil
}

func setupTestServer() (*Server, *mockAdminManager) {
	mockMgr := &mockAdminManager{
		feeds: []web.FeedSummary{
			{ID: "TEST", Title: "Test Feed", URL: "https://youtube.com/channel/test"},
		},
		stats: &web.SystemStats{
			Version:   "v2.8.0",
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

	t.Run("get_system_stats", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_system_stats","arguments":{}}}`
		respBytes, err := server.HandleMessage(ctx, []byte(req))
		require.NoError(t, err)

		var resp JSONRPCResponse
		err = json.Unmarshal(respBytes, &resp)
		require.NoError(t, err)
		assert.Nil(t, resp.Error)
		resData, _ := json.Marshal(resp.Result)
		assert.Contains(t, string(resData), "v2.8.0")
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
