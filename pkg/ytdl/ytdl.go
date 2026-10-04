package ytdl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/model"
)

const (
	DefaultDownloadTimeout = 10 * time.Minute
	UpdatePeriod           = 24 * time.Hour
)

type PlaylistMetadataThumbnail struct {
	Id         string `json:"id"`
	Url        string `json:"url"`
	Resolution string `json:"resolution"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

type PlaylistMetadata struct {
	Id          string                      `json:"id"`
	Title       string                      `json:"title"`
	Description string                      `json:"description"`
	Thumbnails  []PlaylistMetadataThumbnail `json:"thumbnails"`
	Channel     string                      `json:"channel"`
	ChannelId   string                      `json:"channel_id"`
	ChannelUrl  string                      `json:"channel_url"`
	WebpageUrl  string                      `json:"webpage_url"`
}

var (
	ErrTooManyRequests = errors.New(http.StatusText(http.StatusTooManyRequests))
)

// Config is a youtube-dl related configuration
type Config struct {
	// SelfUpdate toggles self update every 24 hour
	SelfUpdate bool `toml:"self_update"`
	// Timeout in minutes for youtube-dl process to finish download
	Timeout int `toml:"timeout"`
	// CustomBinary is a custom path to youtube-dl, this allows using various youtube-dl forks.
	CustomBinary string `toml:"custom_binary"`
	// CookiesFile is the path to Netscape cookies.txt file for yt-dlp authentication
	CookiesFile string `toml:"cookies_file"`
	// CookiesFromBrowser specifies browser to extract cookies from (e.g. "chrome", "firefox", "safari")
	CookiesFromBrowser string `toml:"cookies_from_browser"`
	// Proxy URL to pass to yt-dlp via --proxy
	Proxy string `toml:"proxy"`
}

type YoutubeDl struct {
	path               string
	timeout            time.Duration
	cookiesFile        string
	cookiesFromBrowser string
	proxy              string
	selfUpdate         bool
	updateLock         sync.Mutex // Don't call youtube-dl while self updating
}

func New(ctx context.Context, cfg Config) (*YoutubeDl, error) {
	var (
		path string
		err  error
	)

	if cfg.CustomBinary != "" {
		path = cfg.CustomBinary

		// Don't update custom youtube-dl binaries.
		log.Warnf("using custom youtube-dl binary, turning self updates off")
		cfg.SelfUpdate = false
	} else {
		path, err = exec.LookPath("yt-dlp")
		if err != nil {
			path, err = exec.LookPath("youtube-dl")
			if err != nil {
				return nil, errors.Wrap(err, "neither yt-dlp nor youtube-dl binary found")
			}
		}

		log.Debugf("found downloader binary at %q", path)
	}

	timeout := DefaultDownloadTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Minute
	}

	log.Debugf("download timeout: %d min(s)", int(timeout.Minutes()))

	ytdl := &YoutubeDl{
		path:               path,
		timeout:            timeout,
		cookiesFile:        cfg.CookiesFile,
		cookiesFromBrowser: cfg.CookiesFromBrowser,
		proxy:              cfg.Proxy,
		selfUpdate:         cfg.SelfUpdate,
	}

	// Make sure youtube-dl exists
	version, err := ytdl.exec(ctx, "--version")
	if err != nil {
		return nil, errors.Wrap(err, "could not find youtube-dl")
	}

	log.Infof("using youtube-dl %s", version)

	if err := ytdl.ensureDependencies(ctx); err != nil {
		return nil, err
	}

	if cfg.SelfUpdate {
		// Do initial blocking update at launch
		if err := ytdl.Update(ctx); err != nil {
			log.WithError(err).Error("failed to update youtube-dl")
		}

		go func() {
			for {
				time.Sleep(UpdatePeriod)

				if err := ytdl.Update(context.Background()); err != nil {
					log.WithError(err).Error("update failed")
				}
			}
		}()
	}

	return ytdl, nil
}

func (dl *YoutubeDl) ensureDependencies(ctx context.Context) error {
	found := false

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		found = true

		output, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
		if err != nil {
			return errors.Wrap(err, "could not get ffmpeg version")
		}

		log.Infof("found ffmpeg: %s", output)
	}

	if path, err := exec.LookPath("avconv"); err == nil {
		found = true

		output, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
		if err != nil {
			return errors.Wrap(err, "could not get avconv version")
		}

		log.Infof("found avconv: %s", output)
	}

	if !found {
		return errors.New("either ffmpeg or avconv required to run Podsync")
	}

	return nil
}

func (dl *YoutubeDl) Update(ctx context.Context) error {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()

	log.Info("updating youtube-dl")
	output, err := dl.exec(ctx, "--update", "--verbose")
	if err != nil {
		log.WithError(err).Error(output)
		return errors.Wrap(err, "failed to self update youtube-dl")
	}

	log.Info(output)
	return nil
}

func (dl *YoutubeDl) PlaylistMetadata(ctx context.Context, url string) (metadata PlaylistMetadata, err error) {
	log.Info("getting playlist metadata for: ", url)
	args := []string{
		"--playlist-items", "0",
		"-J",            // JSON output
		"-q",            // quiet mode
		"--no-warnings", // suppress warnings
	}
	dl.updateLock.Lock()
	args = dl.appendCommonArgs(args)
	dl.updateLock.Unlock()
	args = append(args, url)

	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	output, err := dl.exec(ctx, args...)
	if err != nil {
		log.WithError(err).Errorf("youtube-dl error: %s", url)

		// YouTube might block host with HTTP Error 429: Too Many Requests
		if strings.Contains(output, "HTTP Error 429") {
			return PlaylistMetadata{}, ErrTooManyRequests
		}

		log.Error(output)
		return PlaylistMetadata{}, errors.New(output)
	}

	var playlistMetadata PlaylistMetadata
	json.Unmarshal([]byte(output), &playlistMetadata)
	return playlistMetadata, nil
}

func (dl *YoutubeDl) Download(ctx context.Context, feedConfig *feed.Config, episode *model.Episode) (r io.ReadCloser, err error) {
	tmpDir, err := os.MkdirTemp("", "podsync-")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get temp dir for download")
	}

	defer func() {
		if err != nil {
			err1 := os.RemoveAll(tmpDir)
			if err1 != nil {
				log.Errorf("could not remove temp dir: %v", err1)
			}
		}
	}()

	baseName := feed.EpisodeBaseName(feedConfig, episode)
	// filePath with YoutubeDl template format
	filePath := filepath.Join(tmpDir, fmt.Sprintf("%s.%s", baseName, "%(ext)s"))

	args := dl.buildArgs(feedConfig, episode, filePath)

	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()

	output, err := dl.exec(ctx, args...)
	if err != nil {
		log.WithError(err).Errorf("youtube-dl error: %s", filePath)

		// YouTube might block host with HTTP Error 429: Too Many Requests
		if strings.Contains(output, "HTTP Error 429") {
			return nil, ErrTooManyRequests
		}

		log.Error(output)

		return nil, errors.New(output)
	}

	// filePath now with the final extension
	filePath = filepath.Join(tmpDir, feed.EpisodeName(feedConfig, episode))
	f, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open downloaded file")
	}

	return &tempFile{File: f, dir: tmpDir}, nil
}

func (dl *YoutubeDl) exec(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dl.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, dl.path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), errors.Wrap(err, "failed to execute youtube-dl")
	}

	return string(output), nil
}

func (dl *YoutubeDl) appendCommonArgs(args []string) []string {
	if dl == nil {
		return args
	}
	if dl.cookiesFile != "" {
		if _, err := os.Stat(dl.cookiesFile); err == nil {
			args = append(args, "--cookies", dl.cookiesFile)
		} else {
			log.Warnf("configured cookies file %q not found on disk", dl.cookiesFile)
		}
	} else if dl.cookiesFromBrowser != "" {
		args = append(args, "--cookies-from-browser", dl.cookiesFromBrowser)
	}

	if dl.proxy != "" {
		args = append(args, "--proxy", dl.proxy)
	}

	return args
}

func (dl *YoutubeDl) buildArgs(feedConfig *feed.Config, episode *model.Episode, outputFilePath string) []string {
	var args []string

	switch feedConfig.Format {
	case model.FormatVideo:
		// Video, mp4, high by default
		format := "bestvideo[ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[ext=mp4][vcodec^=avc1]/best[ext=mp4]/best"

		if feedConfig.Quality == model.QualityLow {
			format = "worstvideo[ext=mp4][vcodec^=avc1]+worstaudio[ext=m4a]/worst[ext=mp4][vcodec^=avc1]/worst[ext=mp4]/worst"
		} else if feedConfig.Quality == model.QualityHigh && feedConfig.MaxHeight > 0 {
			format = fmt.Sprintf("bestvideo[height<=%d][ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[height<=%d][ext=mp4][vcodec^=avc1]/best[ext=mp4]/best", feedConfig.MaxHeight, feedConfig.MaxHeight)
		}

		args = append(args, "--format", format)

	case model.FormatAudio:
		// Audio, mp3, high by default
		format := "bestaudio"
		if feedConfig.Quality == model.QualityLow {
			format = "worstaudio"
		}

		args = append(args, "--extract-audio", "--audio-format", "mp3", "--format", format)

	default:
		args = append(args, "--audio-format", feedConfig.CustomFormat.Extension, "--format", feedConfig.CustomFormat.YouTubeDLFormat)
	}

	// Insert additional per-feed youtube-dl arguments
	args = append(args, feedConfig.YouTubeDLArgs...)

	// Insert global downloader options (cookies, proxy)
	if dl != nil {
		args = dl.appendCommonArgs(args)
	}

	args = append(args, "--output", outputFilePath, episode.VideoURL)
	return args
}

func buildArgs(feedConfig *feed.Config, episode *model.Episode, outputFilePath string) []string {
	return (*YoutubeDl)(nil).buildArgs(feedConfig, episode, outputFilePath)
}

func (dl *YoutubeDl) GetConfig() Config {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	return Config{
		SelfUpdate:         dl.selfUpdate,
		Timeout:            int(dl.timeout.Minutes()),
		CustomBinary:       dl.path,
		CookiesFile:        dl.cookiesFile,
		CookiesFromBrowser: dl.cookiesFromBrowser,
		Proxy:              dl.proxy,
	}
}

func (dl *YoutubeDl) UpdateConfig(cfg Config) {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	if cfg.Timeout > 0 {
		dl.timeout = time.Duration(cfg.Timeout) * time.Minute
	}
	dl.selfUpdate = cfg.SelfUpdate
	dl.cookiesFile = cfg.CookiesFile
	dl.cookiesFromBrowser = cfg.CookiesFromBrowser
	dl.proxy = cfg.Proxy
}

func (dl *YoutubeDl) SetCookiesFile(path string) {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	dl.cookiesFile = path
}

func (dl *YoutubeDl) SetCookiesFromBrowser(browser string) {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	dl.cookiesFromBrowser = browser
}

func (dl *YoutubeDl) SetProxy(proxy string) {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	dl.proxy = proxy
}

func (dl *YoutubeDl) GetBinaryPath() string {
	return dl.path
}

func (dl *YoutubeDl) GetVersion(ctx context.Context) (string, error) {
	dl.updateLock.Lock()
	defer dl.updateLock.Unlock()
	return dl.exec(ctx, "--version")
}

func (dl *YoutubeDl) TestURL(ctx context.Context, testURL string) (string, error) {
	if testURL == "" {
		testURL = "https://www.youtube.com/watch?v=w9h5wyk-rdg"
	}
	args := []string{
		"--dump-json",
		"--no-download",
		"--no-warnings",
	}
	dl.updateLock.Lock()
	args = dl.appendCommonArgs(args)
	dl.updateLock.Unlock()
	args = append(args, testURL)

	output, err := dl.exec(ctx, args...)
	if err != nil {
		return output, err
	}
	return output, nil
}
