package ytdl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain doubles as a fake yt-dlp when the package tests re-execute the test
// binary as the downloader. This keeps the retry path cross-platform.
func TestMain(m *testing.M) {
	if os.Getenv("PODSYNC_FAKE_YTDL") == "1" {
		fakeYoutubeDl()
		return
	}
	os.Exit(m.Run())
}

func fakeYoutubeDl() {
	args := os.Args[1:]
	statePath := os.Getenv("PODSYNC_FAKE_YTDL_STATE")
	calls := 0
	if data, err := os.ReadFile(statePath); err == nil {
		calls, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	calls++
	_ = os.WriteFile(statePath, []byte(strconv.Itoa(calls)), 0644)

	var (
		hasExtractor   bool
		outputTemplate string
		audioFormat    string
	)
	for i, arg := range args {
		switch arg {
		case "--extractor-args":
			hasExtractor = true
		case "--output":
			if i+1 < len(args) {
				outputTemplate = args[i+1]
			}
		case "--audio-format":
			if i+1 < len(args) {
				audioFormat = args[i+1]
			}
		}
	}
	if hasExtractor {
		fmt.Fprintln(os.Stderr, "ERROR: unable to download video data: HTTP Error 403: Forbidden")
		os.Exit(1)
	}

	ext := "mp4"
	if audioFormat == "mp3" {
		ext = "mp3"
	}
	path := strings.ReplaceAll(outputTemplate, "%(ext)s", ext)
	if err := os.WriteFile(path, []byte("downloaded-media"), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func TestBuildArgsRelaxedFormat(t *testing.T) {
	dl := &YoutubeDl{language: "ru"}
	episode := &model.Episode{VideoURL: "https://youtu.be/example"}

	audio := &feed.Config{Format: model.FormatAudio, Quality: model.QualityHigh}
	args := dl.buildArgsWithOptions(audio, episode, "/tmp/out.mp3", downloadOptions{relaxed: true})
	assert.Contains(t, args, "--extract-audio")
	assert.Contains(t, args, "--format")
	for _, arg := range args {
		assert.NotContains(t, arg, "[language^=")
		assert.NotContains(t, arg, "youtube:lang=")
	}
	for i, arg := range args {
		if arg == "--format" {
			assert.Equal(t, "best[ext=mp4]/best", args[i+1])
		}
	}

	video := &feed.Config{Format: model.FormatVideo, Quality: model.QualityHigh, MaxHeight: 720}
	videoArgs := dl.buildArgsWithOptions(video, episode, "/tmp/out.mp4", downloadOptions{relaxed: true})
	for i, arg := range videoArgs {
		if arg == "--format" {
			assert.Equal(t, "best[height<=720][ext=mp4][vcodec^=avc1]/best[height<=720][ext=mp4]/best[height<=720]/best", videoArgs[i+1])
		}
	}
}

func TestCanRetryRelaxed(t *testing.T) {
	youtubeEpisode := &model.Episode{VideoURL: "https://youtu.be/example"}
	forbidden := "ERROR: unable to download video data: HTTP Error 403: Forbidden"
	audio := func() *feed.Config {
		return &feed.Config{Format: model.FormatAudio, Quality: model.QualityHigh}
	}

	tests := []struct {
		name      string
		language  string
		feed      *feed.Config
		episode   *model.Episode
		output    string
		wantRetry bool
	}{
		{name: "youtube language 403", language: "ru", feed: audio(), episode: youtubeEpisode, output: forbidden, wantRetry: true},
		{name: "per-feed language 403", feed: func() *feed.Config { f := audio(); f.Custom.Language = "en"; return f }(), episode: youtubeEpisode, output: forbidden, wantRetry: true},
		{name: "non-youtube 403", language: "ru", feed: audio(), episode: &model.Episode{VideoURL: "https://vkvideo.ru/video-1_2"}, output: forbidden},
		{name: "no language", feed: audio(), episode: youtubeEpisode, output: forbidden},
		{name: "user extractor args", language: "ru", feed: func() *feed.Config {
			f := audio()
			f.YouTubeDLArgs = []string{"--extractor-args", "youtube:player_client=tv"}
			return f
		}(), episode: youtubeEpisode, output: forbidden},
		{name: "custom format", language: "ru", feed: &feed.Config{Format: "custom", CustomFormat: feed.CustomFormat{Extension: "m4a", YouTubeDLFormat: "bestaudio"}}, episode: youtubeEpisode, output: forbidden},
		{name: "too many requests", language: "ru", feed: audio(), episode: youtubeEpisode, output: "HTTP Error 429: Too Many Requests"},
		{name: "format unavailable", language: "ru", feed: audio(), episode: youtubeEpisode, output: "ERROR: Requested format is not available", wantRetry: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dl := &YoutubeDl{language: tc.language}
			assert.Equal(t, tc.wantRetry, dl.canRetryRelaxed(tc.feed, tc.episode, tc.output))
		})
	}
}

func TestIsForbiddenDownloadError(t *testing.T) {
	assert.True(t, isForbiddenDownloadError("HTTP Error 403: Forbidden"))
	assert.True(t, isForbiddenDownloadError("Server returned 403 Forbidden (access denied)"))
	assert.True(t, isForbiddenDownloadError("ERROR: Requested format is not available"))
	assert.False(t, isForbiddenDownloadError("HTTP Error 429: Too Many Requests"))
	assert.False(t, isForbiddenDownloadError("Sign in to confirm you’re not a bot"))
}

func TestDownloadRetriesWithoutLanguageOnForbidden(t *testing.T) {
	state := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PODSYNC_FAKE_YTDL", "1")
	t.Setenv("PODSYNC_FAKE_YTDL_STATE", state)

	dl := &YoutubeDl{
		path:     os.Args[0],
		language: "ru",
		timeout:  time.Minute,
	}
	feedConfig := &feed.Config{ID: "test", Format: model.FormatAudio, Quality: model.QualityHigh}
	episode := &model.Episode{ID: "episode-1", Title: "Episode", VideoURL: "https://youtu.be/example"}

	reader, err := dl.Download(context.Background(), feedConfig, episode)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "downloaded-media", string(data))

	calls, err := os.ReadFile(state)
	require.NoError(t, err)
	assert.Equal(t, "2", strings.TrimSpace(string(calls)), "the first attempt must be retried exactly once")
}
