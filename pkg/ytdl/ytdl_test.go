package ytdl

import (
	"testing"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"

	"github.com/stretchr/testify/assert"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name         string
		format       model.Format
		customFormat feed.CustomFormat
		quality      model.Quality
		maxHeight    int
		output       string
		videoURL     string
		ytdlArgs     []string
		expect       []string
	}{
		{
			name:     "Audio unknown quality",
			format:   model.FormatAudio,
			output:   "/tmp/1",
			videoURL: "http://url",
			expect:   []string{"--extract-audio", "--audio-format", "mp3", "--format", "bestaudio/best", "--output", "/tmp/1", "http://url"},
		},
		{
			name:     "Audio low quality",
			format:   model.FormatAudio,
			quality:  model.QualityLow,
			output:   "/tmp/1",
			videoURL: "http://url",
			expect:   []string{"--extract-audio", "--audio-format", "mp3", "--format", "worstaudio/worst", "--output", "/tmp/1", "http://url"},
		},
		{
			name:     "Audio best quality",
			format:   model.FormatAudio,
			quality:  model.QualityHigh,
			output:   "/tmp/1",
			videoURL: "http://url",
			expect:   []string{"--extract-audio", "--audio-format", "mp3", "--format", "bestaudio/best", "--output", "/tmp/1", "http://url"},
		},
		{
			name:     "Video unknown quality",
			format:   model.FormatVideo,
			output:   "/tmp/1",
			videoURL: "http://url",
			expect:   []string{"--format", "bestvideo[ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", "--output", "/tmp/1", "http://url"},
		},
		{
			name:      "Video unknown quality with maxheight",
			format:    model.FormatVideo,
			maxHeight: 720,
			output:    "/tmp/1",
			videoURL:  "http://url",
			expect:    []string{"--format", "bestvideo[ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", "--output", "/tmp/1", "http://url"},
		},
		{
			name:     "Video low quality",
			format:   model.FormatVideo,
			quality:  model.QualityLow,
			output:   "/tmp/2",
			videoURL: "http://url",
			expect:   []string{"--format", "worstvideo[ext=mp4][vcodec^=avc1]+worstaudio[ext=m4a]/worst[ext=mp4][vcodec^=avc1]/worst[ext=mp4]/worst", "--output", "/tmp/2", "http://url"},
		},
		{
			name:      "Video low quality with maxheight",
			format:    model.FormatVideo,
			quality:   model.QualityLow,
			maxHeight: 720,
			output:    "/tmp/2",
			videoURL:  "http://url",
			expect:    []string{"--format", "worstvideo[ext=mp4][vcodec^=avc1]+worstaudio[ext=m4a]/worst[ext=mp4][vcodec^=avc1]/worst[ext=mp4]/worst", "--output", "/tmp/2", "http://url"},
		},
		{
			name:     "Video high quality",
			format:   model.FormatVideo,
			quality:  model.QualityHigh,
			output:   "/tmp/2",
			videoURL: "http://url1",
			expect:   []string{"--format", "bestvideo[ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", "--output", "/tmp/2", "http://url1"},
		},
		{
			name:      "Video high quality with maxheight",
			format:    model.FormatVideo,
			quality:   model.QualityHigh,
			maxHeight: 1024,
			output:    "/tmp/2",
			videoURL:  "http://url1",
			expect:    []string{"--format", "bestvideo[height<=1024][ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[height<=1024][ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", "--output", "/tmp/2", "http://url1"},
		},
		{
			name:     "Video high quality with custom youtube-dl arguments",
			format:   model.FormatVideo,
			quality:  model.QualityHigh,
			output:   "/tmp/2",
			videoURL: "http://url1",
			ytdlArgs: []string{"--write-sub", "--embed-subs", "--sub-lang", "en,en-US,en-GB"},
			expect:   []string{"--format", "bestvideo[ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", "--write-sub", "--embed-subs", "--sub-lang", "en,en-US,en-GB", "--output", "/tmp/2", "http://url1"},
		},
		{
			name:         "Custom format",
			format:       model.FormatCustom,
			customFormat: feed.CustomFormat{YouTubeDLFormat: "bestaudio[ext=m4a]", Extension: "m4a"},
			quality:      model.QualityHigh,
			output:       "/tmp/2",
			videoURL:     "http://url1",
			expect:       []string{"--audio-format", "m4a", "--format", "bestaudio[ext=m4a]", "--output", "/tmp/2", "http://url1"},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			result := (*YoutubeDl)(nil).buildArgs(&feed.Config{
				Format:        tst.format,
				Quality:       tst.quality,
				CustomFormat:  tst.customFormat,
				MaxHeight:     tst.maxHeight,
				YouTubeDLArgs: tst.ytdlArgs,
			}, &model.Episode{
				VideoURL: tst.videoURL,
			}, tst.output)

			assert.EqualValues(t, tst.expect, result)
		})
	}
}

func TestBuildArgsWithCookiesAndProxy(t *testing.T) {
	dl := &YoutubeDl{
		cookiesFromBrowser: "chrome",
		proxy:              "http://proxy:8080",
	}

	feedCfg := &feed.Config{
		Format:  model.FormatAudio,
		Quality: model.QualityHigh,
	}
	ep := &model.Episode{
		VideoURL: "https://youtube.com/watch?v=123",
	}

	args := dl.buildArgs(feedCfg, ep, "/tmp/out.mp3")
	assert.Contains(t, args, "--cookies-from-browser")
	assert.Contains(t, args, "chrome")
	assert.Contains(t, args, "--proxy")
	assert.Contains(t, args, "http://proxy:8080")

	// Feed with explicit --cookies in YouTubeDLArgs should not have --cookies-from-browser appended
	feedWithCookies := &feed.Config{
		Format:        model.FormatAudio,
		Quality:       model.QualityHigh,
		YouTubeDLArgs: []string{"--cookies", "/custom/cookies.txt"},
	}
	args2 := dl.buildArgs(feedWithCookies, ep, "/tmp/out.mp3")
	assert.Contains(t, args2, "/custom/cookies.txt")
	assert.NotContains(t, args2, "--cookies-from-browser")
}

func TestCleanErrorMessage(t *testing.T) {
	rawBot := `[youtube] Extracting URL: https://youtube.com/watch?v=FDgrwfKOkG8
[youtube] FDgrwfKOkG8: Downloading webpage
ERROR: [youtube] FDgrwfKOkG8: Sign in to confirm you’re not a bot. This helps protect our community.`
	msg := CleanErrorMessage(rawBot)
	assert.Contains(t, msg, "sign in to confirm you are not a bot")
	assert.Contains(t, msg, "configure cookies")

	raw429 := `[youtube] FDgrwfKOkG8: Downloading webpage
ERROR: HTTP Error 429: Too Many Requests`
	msg429 := CleanErrorMessage(raw429)
	assert.Contains(t, msg429, "Rate limit: HTTP Error 429")

	rawSimple := `[youtube] Extracting URL: https://youtube.com/watch?v=123
ERROR: [youtube] 123: Private video. Sign in if you have been granted access to this video`
	msgSimple := CleanErrorMessage(rawSimple)
	assert.Equal(t, "123: Private video. Sign in if you have been granted access to this video", msgSimple)
}

func TestBuildArgsWithLanguage(t *testing.T) {
	dl := &YoutubeDl{
		language: "ru",
	}

	ep := &model.Episode{
		VideoURL: "https://youtube.com/watch?v=mr7vYgrUjPI",
	}

	// 1. Video format with global language "ru"
	videoFeed := &feed.Config{
		Format:  model.FormatVideo,
		Quality: model.QualityHigh,
	}
	videoArgs := dl.buildArgs(videoFeed, ep, "/tmp/out.mp4")
	assert.Contains(t, videoArgs, "--format")
	for i, a := range videoArgs {
		if a == "--format" {
			assert.Contains(t, videoArgs[i+1], "[language^=ru]")
			break
		}
	}
	assert.Contains(t, videoArgs, "--extractor-args")
	assert.Contains(t, videoArgs, "youtube:lang=ru")

	// 2. Audio format with global language "ru"
	audioFeed := &feed.Config{
		Format:  model.FormatAudio,
		Quality: model.QualityHigh,
	}
	audioArgs := dl.buildArgs(audioFeed, ep, "/tmp/out.mp3")
	assert.Contains(t, audioArgs, "bestaudio[language^=ru]/bestaudio/best")
	assert.Contains(t, audioArgs, "--extractor-args")
	assert.Contains(t, audioArgs, "youtube:lang=ru")

	// 3. Per-feed Custom.Language "en" overrides global "ru"
	enFeed := &feed.Config{
		Format:  model.FormatVideo,
		Quality: model.QualityHigh,
		Custom: feed.Custom{
			Language: "en",
		},
	}
	enArgs := dl.buildArgs(enFeed, ep, "/tmp/out.mp4")
	for i, a := range enArgs {
		if a == "--format" {
			assert.Contains(t, enArgs[i+1], "[language^=en]")
			break
		}
	}
	assert.Contains(t, enArgs, "youtube:lang=en")
	assert.NotContains(t, enArgs, "youtube:lang=ru")

	// 4. If --extractor-args already present in YouTubeDLArgs, do not duplicate
	customArgsFeed := &feed.Config{
		Format:        model.FormatAudio,
		YouTubeDLArgs: []string{"--extractor-args", "youtube:player_client=android"},
	}
	customArgs := dl.buildArgs(customArgsFeed, ep, "/tmp/out.mp3")
	count := 0
	for _, a := range customArgs {
		if a == "--extractor-args" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestBuildArgsWithIgnoreLive(t *testing.T) {
	dl := &YoutubeDl{}
	feedCfg := &feed.Config{
		Format:     model.FormatAudio,
		Quality:    model.QualityHigh,
		IgnoreLive: true,
	}
	ep := &model.Episode{
		VideoURL: "https://youtube.com/watch?v=live123",
	}
	args := dl.buildArgs(feedCfg, ep, "/tmp/out.mp3")
	assert.Contains(t, args, "--match-filters")
	assert.Contains(t, args, "!is_live & live_status !=? 'is_upcoming' & live_status !=? 'is_live'")
}

func TestBuildArgsWithNormalizeAudio(t *testing.T) {
	dl := &YoutubeDl{}
	feedCfg := &feed.Config{
		Format:         model.FormatAudio,
		Quality:        model.QualityHigh,
		NormalizeAudio: true,
	}
	ep := &model.Episode{
		VideoURL: "https://youtube.com/watch?v=audio123",
	}
	args := dl.buildArgs(feedCfg, ep, "/tmp/out.mp3")
	assert.Contains(t, args, "--postprocessor-args")
	assert.Contains(t, args, "ExtractAudio:-af loudnorm=I=-16:TP=-1.5:LRA=11")
}

func TestProxyRouting(t *testing.T) {
	dl := &YoutubeDl{
		proxy: "http://default-proxy:8080",
		proxies: map[string]string{
			"youtube": "socks5://127.0.0.1:1080",
			"vkvideo": "direct",
			"rutube":  "direct",
			"vimeo":   "http://vimeo-proxy:3128",
		},
	}

	// 1. YouTube URL uses YouTube proxy
	assert.Equal(t, "socks5://127.0.0.1:1080", dl.ProxyForURL("https://www.youtube.com/watch?v=123", ""))
	assert.Equal(t, "socks5://127.0.0.1:1080", dl.ProxyForURL("https://youtu.be/123", ""))

	// 2. VK Video and Rutube bypass proxy with direct
	assert.Equal(t, "", dl.ProxyForURL("https://vkvideo.ru/video-123_456", ""))
	assert.Equal(t, "", dl.ProxyForURL("https://rutube.ru/video/abc/", ""))

	// 3. Vimeo uses Vimeo proxy
	assert.Equal(t, "http://vimeo-proxy:3128", dl.ProxyForURL("https://vimeo.com/76979871", ""))

	// 4. Unknown domain falls back to global proxy
	assert.Equal(t, "http://default-proxy:8080", dl.ProxyForURL("https://example.com/video.mp4", ""))

	// 5. Per-feed proxy override takes precedence over everything
	assert.Equal(t, "http://custom-feed-proxy:9090", dl.ProxyForURL("https://youtube.com/watch?v=123", "http://custom-feed-proxy:9090"))
	assert.Equal(t, "", dl.ProxyForURL("https://youtube.com/watch?v=123", "direct"))
}

func TestBuildArgsDirectProxyBypassesGlobalProxy(t *testing.T) {
	dl := &YoutubeDl{proxy: "http://default-proxy:8080", proxies: map[string]string{"vkvideo": "direct"}}
	feedCfg := &feed.Config{Format: model.FormatAudio, Quality: model.QualityHigh}

	providerDirect := dl.buildArgs(feedCfg, &model.Episode{VideoURL: "https://vkvideo.ru/video-1_2"}, "/tmp/out.mp3")
	assert.NotContains(t, providerDirect, "http://default-proxy:8080")
	assert.NotContains(t, providerDirect, "--proxy")

	feedCfg.Proxy = "direct"
	feedDirect := dl.buildArgs(feedCfg, &model.Episode{VideoURL: "https://youtube.com/watch?v=123"}, "/tmp/out.mp3")
	assert.NotContains(t, feedDirect, "http://default-proxy:8080")
	assert.NotContains(t, feedDirect, "--proxy")
}
