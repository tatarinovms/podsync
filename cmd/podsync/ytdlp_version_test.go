package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestYtDlpUpdateAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"2026.10.26", "2026.11.23", true},
		{"2026.11.23", "2026.11.23", false},
		{"2026.12.01", "2026.11.23", false},
		{"2026.9.30", "2026.10.01", true},
		{"v2026.10.26", "2026.11.23", true},
		{"nightly", "2026.11.23", true},
		{"", "2026.11.23", true},
		{"2026.11.23", "nightly", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ytDlpUpdateAvailable(tc.current, tc.latest), "%q -> %q", tc.current, tc.latest)
	}
}

func TestLatestYtDlpRelease(t *testing.T) {
	serve := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
			require.NotEmpty(t, r.Header.Get("User-Agent"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		}))
	}

	server := serve(http.StatusOK, `{"tag_name":"2026.11.23","html_url":"https://github.com/yt-dlp/yt-dlp/releases/tag/2026.11.23"}`)
	defer server.Close()
	oldURL := ytDlpLatestReleaseURL
	ytDlpLatestReleaseURL = server.URL
	t.Cleanup(func() { ytDlpLatestReleaseURL = oldURL })

	tag, url, err := latestYtDlpRelease(context.Background())
	require.NoError(t, err)
	require.Equal(t, "2026.11.23", tag)
	require.Equal(t, "https://github.com/yt-dlp/yt-dlp/releases/tag/2026.11.23", url)

	bad := serve(http.StatusBadGateway, `{}`)
	defer bad.Close()
	ytDlpLatestReleaseURL = bad.URL
	_, _, err = latestYtDlpRelease(context.Background())
	require.ErrorContains(t, err, "HTTP 502")

	invalid := serve(http.StatusOK, `{"tag_name":""}`)
	defer invalid.Close()
	ytDlpLatestReleaseURL = invalid.URL
	_, _, err = latestYtDlpRelease(context.Background())
	require.ErrorContains(t, err, "failed to read")
}

func TestCheckYtDlpUpdateWithoutDownloader(t *testing.T) {
	manager := &AppFeedManager{}
	_, err := manager.CheckYtDlpUpdate(context.Background())
	require.ErrorContains(t, err, "not configured")
}
