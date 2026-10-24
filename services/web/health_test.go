package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthUsesFailureTimeInsteadOfPublicationTime(t *testing.T) {
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	defer database.Close()
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	f := &model.Feed{ID: "feed", Episodes: []*model.Episode{{ID: "old", PubDate: old, Status: model.EpisodeError, ErrorAt: &now}}}
	require.NoError(t, database.AddFeed(context.Background(), f.ID, f))
	srv := New(Config{}, &mockFileSystem{}, database)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NoError(t, database.UpdateEpisode(f.ID, "old", func(ep *model.Episode) error {
		ep.PubDate = now
		ep.ErrorAt = &old
		return nil
	}))
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestPublicURLIncludesMountOnce(t *testing.T) {
	assert.Equal(t, "https://example.com/podcasts", (Config{Hostname: "https://example.com", Path: "podcasts"}).PublicURL())
	assert.Equal(t, "https://example.com/podcasts", (Config{Hostname: "https://example.com/podcasts/", Path: "podcasts"}).PublicURL())
}

func TestHealthLiveAndReady(t *testing.T) {
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	closed := false
	t.Cleanup(func() {
		if !closed {
			require.NoError(t, database.Close())
		}
	})

	srv := New(Config{}, &mockFileSystem{}, database)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })

	// 1. /health/live
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health/live", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)

	// 2. /health/ready
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health/ready", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ready"`)

	require.NoError(t, database.Close())
	closed = true
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health/ready", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health/live", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
