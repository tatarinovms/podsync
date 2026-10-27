package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/personal"
	"github.com/mxpv/podsync/services/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsReachableUsesReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Download failures can make the legacy diagnostic endpoint return 503
		// while the running daemon remains available to MCP clients.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	assert.True(t, NewHTTPClientManager(server.URL, "", "").IsReachable(context.Background()))
}

func TestStdioProxyPersonalFeeds(t *testing.T) {
	ctx := context.Background()
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	files, err := fs.NewLocal(t.TempDir(), false, true)
	require.NoError(t, err)
	service, err := personal.New(ctx, database, files, nil, "https://example.org", nil, nil)
	require.NoError(t, err)
	server, mgr := setupTestServer()
	server.SetPersonalFeeds(service)
	server.SetVersion("v2.22.0")
	app := web.New(web.Config{Admin: web.AdminConfig{Enabled: true, Username: "test-user", Password: "test-password"}}, files, database)
	app.SetAdminManager(mgr)
	app.SetMCPHandler(NewHTTPHandler(server))
	t.Cleanup(func() { require.NoError(t, app.Close()) })
	httpServer := httptest.NewServer(app.Handler)
	t.Cleanup(httpServer.Close)
	proxy := NewHTTPClientManager(httpServer.URL, "test-user", "test-password")
	require.True(t, proxy.IsReachable(ctx))

	relay := func(payload string) []byte {
		t.Helper()
		var output bytes.Buffer
		require.NoError(t, RunStdio(ctx, proxy, strings.NewReader(payload+"\n"), &output))
		if output.Len() > 0 {
			assert.Equal(t, 1, bytes.Count(output.Bytes(), []byte("\n")), "stdio must return one line per response")
		}
		return bytes.TrimSpace(output.Bytes())
	}
	// Metadata and read-only operations must match direct MCP, including the
	// daemon's version, which may differ from the stdio client's build version.
	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"podsync://feeds"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"summarize_library"}}`,
	} {
		direct, err := server.HandleMessage(ctx, []byte(payload))
		require.NoError(t, err)
		assert.JSONEq(t, string(direct), string(relay(payload)))
	}
	assert.Empty(t, relay(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))

	call := func(name string, args map[string]interface{}, out interface{}) {
		t.Helper()
		payload, err := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0", "id": "personal-request", "method": "tools/call",
			"params": map[string]interface{}{"name": name, "arguments": args},
		})
		require.NoError(t, err)
		var response struct {
			ID     string         `json:"id"`
			Result CallToolResult `json:"result"`
			Error  *JSONRPCError  `json:"error"`
		}
		require.NoError(t, json.Unmarshal(relay(string(payload)), &response))
		require.Nil(t, response.Error)
		require.False(t, response.Result.IsError, "%s: %+v", name, response.Result.Content)
		assert.Equal(t, "personal-request", response.ID)
		require.Len(t, response.Result.Content, 1)
		if out != nil {
			require.NoError(t, json.Unmarshal([]byte(response.Result.Content[0].Text), out))
		}
	}
	var created personal.Feed
	call("create_personal_feed", map[string]interface{}{"title": "Proxy feed", "private_feed": true}, &created)
	assert.Equal(t, "Proxy feed", created.Title)
	assert.True(t, created.PrivateFeed)
	var feeds []*personal.Feed
	call("list_personal_feeds", nil, &feeds)
	assert.Len(t, feeds, 2) // Default personal feed and the newly created feed.

	var episode model.Episode
	call("add_personal_episode", map[string]interface{}{
		"feed_id": created.ID, "url": "https://example.org/audio.mp3", "title": "URL episode", "format": "audio", "quality": "low", "max_height": 480,
	}, &episode)
	var media personal.Media
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+episode.MediaID, &media) }))
	assert.Equal(t, model.QualityLow, media.Quality)
	assert.Equal(t, 480, media.MaxHeight)
	call("rename_personal_episode", map[string]interface{}{"feed_id": created.ID, "episode_id": episode.ID, "title": "Renamed through stdio"}, &episode)
	assert.Equal(t, "Renamed through stdio", episode.Title)
	var detail personal.Detail
	call("get_personal_feed", map[string]interface{}{"feed_id": created.ID}, &detail)
	require.Len(t, detail.Episodes, 1)
	assert.Equal(t, episode.Title, detail.Episodes[0].Title)
	call("delete_personal_episode", map[string]interface{}{"feed_id": created.ID, "episode_id": episode.ID}, nil)
	call("get_personal_feed", map[string]interface{}{"feed_id": created.ID}, &detail)
	assert.Empty(t, detail.Episodes)

	// Local imports are handled on the daemon's filesystem, too.
	sourcePath := filepath.Join(t.TempDir(), "source.mp3")
	require.NoError(t, os.WriteFile(sourcePath, []byte("test source"), 0600))
	call("add_personal_episode", map[string]interface{}{"feed_id": created.ID, "file_path": sourcePath, "title": "Local episode"}, &episode)
	assert.Equal(t, "Local episode", episode.Title)

	// Tool errors remain JSON-RPC tool errors and do not terminate the proxy.
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_personal_feed","arguments":{"feed_id":%q}}}`, "missing-feed")
	direct, err := server.HandleMessage(ctx, []byte(payload))
	require.NoError(t, err)
	assert.JSONEq(t, string(direct), string(relay(payload)))
}

func TestStdioProxyHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError string
	}{
		{"unauthorized", http.StatusUnauthorized, "Unauthorized", "HTTP 401"},
		{"unavailable", http.StatusServiceUnavailable, "Unavailable", "HTTP 503"},
		{"invalid_response", http.StatusOK, "not JSON", "invalid MCP response"},
		{"formatted_response", http.StatusOK, "{\n  \"jsonrpc\": \"2.0\", \"id\": 1, \"result\": {}\n}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/mcp", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			var output bytes.Buffer
			err := RunStdio(context.Background(), NewHTTPClientManager(server.URL, "", ""), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), &output)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				assert.Empty(t, output.String())
			} else {
				require.NoError(t, err)
				assert.Equal(t, 1, strings.Count(output.String(), "\n"))
				assert.JSONEq(t, tc.body, output.String())
			}
		})
	}
}
