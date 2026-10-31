package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/personal"
	"github.com/stretchr/testify/require"
)

func TestPersonalFeedAPI(t *testing.T) {
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	defer database.Close()
	files, err := fs.NewLocal(t.TempDir(), false, true)
	require.NoError(t, err)
	service, err := personal.New(context.Background(), database, files, nil, "https://example.org", nil, nil)
	require.NoError(t, err)
	s := &Server{personal: service}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/personal-feeds"+path, strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handlePersonalFeeds(w, r)
		return w
	}
	require.Equal(t, 200, call("GET", "", "").Code)
	require.Equal(t, 400, call("POST", "", `{"title":"   "}`).Code)
	w := call("POST", "", `{"title":"IT","format":"audio"}`)
	require.Equal(t, 201, w.Code, w.Body.String())
	var f personal.Feed
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &f))
	require.Equal(t, 400, call("PATCH", "/"+f.ID, `{"id":"renamed"}`).Code)
	require.Equal(t, 400, call("PATCH", "/"+f.ID, `{"title":""}`).Code)
	require.Equal(t, 200, call("PATCH", "/"+f.ID, `{"description":"Updated"}`).Code)
	w = call("POST", "/personal/episodes", `{"url":"https://example.org/video"}`)
	require.Equal(t, 202, w.Code, w.Body.String())
	var episode struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &episode))
	require.Equal(t, 200, call("POST", "/personal/episodes/"+episode.ID+"/move", `{"target_feed_id":"`+f.ID+`"}`).Code)
	require.Equal(t, 404, call("POST", "/personal/episodes/"+episode.ID+"/retry", "").Code)
	require.Equal(t, 200, call("PATCH", "/"+f.ID+"/episodes/"+episode.ID, `{"title":"Renamed Episode"}`).Code)
	require.Equal(t, 200, call("POST", "/"+f.ID+"/episodes/"+episode.ID+"/rename", `{"title":"Renamed Again"}`).Code)
	require.Equal(t, 400, call("PATCH", "/"+f.ID+"/episodes/"+episode.ID, `{"title":""}`).Code)
	require.Equal(t, 400, call("DELETE", "/personal", "").Code)
	require.Equal(t, 204, call("DELETE", "/personal?confirm=true&delete_files=true", "").Code)
	require.Equal(t, 404, call("GET", "/personal", "").Code)
	require.Equal(t, 404, call("GET", "/missing/episodes", "").Code)
	require.Equal(t, 405, call(http.MethodPut, "/"+f.ID, `{}`).Code)
	require.Equal(t, 400, call("PATCH", "/"+f.ID, `{} {}`).Code)

	// Quick-Add endpoint tests
	detail, err := service.Get(f.ID)
	require.NoError(t, err)
	require.NotEmpty(t, detail.Feed.QuickAddToken)

	// 1. Missing token -> 401
	require.Equal(t, 401, call("POST", "/"+f.ID+"/quick-add?url=https://example.org/media.mp3", "").Code)
	// 2. Invalid token -> 401
	require.Equal(t, 401, call("POST", "/"+f.ID+"/quick-add?token=badtoken&url=https://example.org/media.mp3", "").Code)
	// 3. Valid token via query param -> 202
	quickW := call("POST", "/"+f.ID+"/quick-add?token="+detail.Feed.QuickAddToken+"&url=https://example.org/media.mp3", "")
	require.Equal(t, 202, quickW.Code, quickW.Body.String())
	// 4. Valid token via GET (for simple shortcuts / bookmarklets) -> 202
	getW := call("GET", "/"+f.ID+"/quick-add?token="+detail.Feed.QuickAddToken+"&url=https://example.org/another.mp3", "")
	require.Equal(t, 202, getW.Code, getW.Body.String())
	require.Equal(t, "no-store", getW.Header().Get("Cache-Control"))
	// Only the documented GET and POST forms may mutate a feed.
	require.Equal(t, 405, call(http.MethodPut, "/"+f.ID+"/quick-add?token="+detail.Feed.QuickAddToken+"&url=https://example.org/nope.mp3", "").Code)
}

func TestPersonalFeedMigratesQuickAddToken(t *testing.T) {
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	defer database.Close()
	files, err := fs.NewLocal(t.TempDir(), false, true)
	require.NoError(t, err)
	require.NoError(t, database.PersonalUpdate(func(tx db.PersonalTx) error {
		return tx.Put("feed/legacy", &personal.Feed{ID: "legacy", Type: "personal", Title: "Legacy", PlaylistSort: "desc"})
	}))
	service, err := personal.New(context.Background(), database, files, nil, "https://example.org", nil, nil)
	require.NoError(t, err)
	detail, err := service.Get("legacy")
	require.NoError(t, err)
	require.NotEmpty(t, detail.Feed.QuickAddToken)
	require.NotEqual(t, "706b5f6c6567616379", detail.Feed.QuickAddToken, "legacy tokens must not be deterministic")
	var stored personal.Feed
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("feed/legacy", &stored) }))
	require.Equal(t, detail.Feed.QuickAddToken, stored.QuickAddToken)
}
