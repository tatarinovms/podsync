package personal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
)

type MediaProcessor struct {
	Downloader *ytdl.YoutubeDl
	Files      fs.Storage
}

type convertedFile struct {
	*os.File
	dir string
}

func (f *convertedFile) Close() error {
	err := f.File.Close()
	returnError := os.RemoveAll(f.dir)
	if err != nil {
		return err
	}
	return returnError
}

func isPlatformURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	domains := []string{
		"youtube.com", "youtu.be",
		"vk.com", "vkvideo.ru", "vk.ru",
		"rutube.ru",
		"vimeo.com",
		"soundcloud.com",
		"tiktok.com",
		"dzen.ru",
		"bilibili.com",
		"twitter.com", "x.com",
		"instagram.com",
		"facebook.com",
	}
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func probeHasVideo(ctx context.Context, input string) bool {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_type", "-of", "csv=p=0", input)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "video"
}

func (p *MediaProcessor) Process(ctx context.Context, m *Media) (io.ReadCloser, error) {
	if m.SourcePath == "" {
		isDirectMedia := false
		if u, err := url.Parse(m.SourceURL); err == nil {
			ext := strings.ToLower(filepath.Ext(u.Path))
			switch ext {
			case ".mp3", ".m4a", ".m4b", ".wav", ".ogg", ".opus", ".flac", ".aac", ".mp4", ".m4v", ".mov", ".webm", ".mkv":
				isDirectMedia = true
			}
		}

		isPlatform := isPlatformURL(m.SourceURL)

		if isPlatform {
			if p.Downloader == nil {
				return nil, fmt.Errorf("downloader is not configured for platform URLs")
			}
			title, description, thumbnail, duration, err := p.Downloader.SingleMetadata(ctx, m.SourceURL)
			if err != nil {
				return nil, fmt.Errorf("metadata extraction failed: %w", err)
			}
			if m.Title == "" || m.Title == m.SourceURL {
				m.Title = title
			}
			m.Description = description
			m.Thumbnail = thumbnail
			m.Duration = duration

			quality := m.Quality
			if quality == "" {
				quality = model.QualityHigh
			}
			cfg := &feed.Config{
				ID:            m.ID,
				Format:        m.Format,
				Quality:       quality,
				MaxHeight:     m.MaxHeight,
				YouTubeDLArgs: []string{"--no-playlist"},
			}
			if m.Format == model.FormatVideo {
				cfg.YouTubeDLArgs = append(cfg.YouTubeDLArgs, "--remux-video", "mp4")
			}
			return p.Downloader.Download(ctx, cfg, &model.Episode{ID: m.ID, VideoURL: m.SourceURL, Title: m.Title})
		}

		if !isDirectMedia && p.Downloader != nil {
			title, description, thumbnail, duration, err := p.Downloader.SingleMetadata(ctx, m.SourceURL)
			if err == nil {
				if m.Title == "" || m.Title == m.SourceURL {
					m.Title = title
				}
				m.Description = description
				m.Thumbnail = thumbnail
				m.Duration = duration

				quality := m.Quality
				if quality == "" {
					quality = model.QualityHigh
				}
				cfg := &feed.Config{
					ID:            m.ID,
					Format:        m.Format,
					Quality:       quality,
					MaxHeight:     m.MaxHeight,
					YouTubeDLArgs: []string{"--no-playlist"},
				}
				if m.Format == model.FormatVideo {
					cfg.YouTubeDLArgs = append(cfg.YouTubeDLArgs, "--remux-video", "mp4")
				}
				return p.Downloader.Download(ctx, cfg, &model.Episode{ID: m.ID, VideoURL: m.SourceURL, Title: m.Title})
			}
		}

		// Direct HTTP download fallback for direct audio/video files
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.SourceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("invalid URL: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download from URL failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
		}

		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if idx := strings.Index(ct, ";"); idx != -1 {
			ct = strings.TrimSpace(ct[:idx])
		}
		if strings.HasPrefix(ct, "text/") || ct == "application/json" || ct == "application/xml" || ct == "application/xhtml+xml" {
			return nil, fmt.Errorf("URL returned web content (%s) instead of media stream; ensure URL points directly to an audio/video file", ct)
		}

		dir, err := os.MkdirTemp("", "podsync-personal-")
		if err != nil {
			return nil, err
		}
		success := false
		defer func() {
			if !success {
				_ = os.RemoveAll(dir)
			}
		}()

		ext := filepath.Ext(m.SourceURL)
		if ext == "" || len(ext) > 5 {
			ext = ".media"
		}
		input := filepath.Join(dir, "input"+ext)
		out, err := os.Create(input)
		if err != nil {
			return nil, err
		}

		headerBuf := make([]byte, 512)
		n, _ := io.ReadFull(resp.Body, headerBuf)
		if n > 0 {
			sniffed := http.DetectContentType(headerBuf[:n])
			if strings.HasPrefix(sniffed, "text/") {
				_ = out.Close()
				return nil, fmt.Errorf("downloaded content is %s, not a valid media file", sniffed)
			}
			if _, err := out.Write(headerBuf[:n]); err != nil {
				_ = out.Close()
				return nil, err
			}
		}

		_, copyErr := io.Copy(out, resp.Body)
		closeErr := out.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		rc, err := p.convertFile(ctx, m, dir, input)
		if err == nil {
			success = true
		}
		return rc, err
	}

	if err := validateMediaPath(m.SourcePath); err != nil {
		return nil, err
	}
	var source io.ReadCloser
	var err error
	if reader, ok := p.Files.(interface {
		Read(context.Context, string) (io.ReadCloser, error)
	}); ok {
		source, err = reader.Read(ctx, m.SourcePath)
	} else {
		source, err = p.Files.Open(m.SourcePath)
	}
	if err != nil {
		return nil, err
	}
	defer source.Close()

	dir, err := os.MkdirTemp("", "podsync-personal-")
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()

	input := filepath.Join(dir, "input"+filepath.Ext(m.SourcePath))
	out, err := os.Create(input)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, source); err != nil {
		_ = out.Close()
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}

	rc, err := p.convertFile(ctx, m, dir, input)
	if err == nil {
		success = true
	}
	return rc, err
}

func (p *MediaProcessor) convertFile(ctx context.Context, m *Media, dir, input string) (io.ReadCloser, error) {
	targetFormat := m.Format
	if targetFormat == "" {
		if probeHasVideo(ctx, input) {
			targetFormat = model.FormatVideo
		} else {
			targetFormat = model.FormatAudio
		}
		m.Format = targetFormat
	}

	outExt := ".mp3"
	if targetFormat == model.FormatVideo {
		outExt = ".mp4"
		m.Path = "_personal/" + m.ID + "/media.mp4"
	} else {
		m.Path = "_personal/" + m.ID + "/media.mp3"
	}
	output := filepath.Join(dir, "output"+outExt)

	quality := m.Quality
	if quality == "" {
		quality = model.QualityHigh
	}

	var cmd *exec.Cmd
	if targetFormat == model.FormatVideo {
		// Output MP4 (H.264 / AAC)
		args := []string{"-y", "-i", input, "-c:v", "libx264"}
		if m.MaxHeight > 0 {
			scaleFilter := fmt.Sprintf("scale=-2:'min(ih,%d)'", m.MaxHeight)
			args = append(args, "-vf", scaleFilter)
		}
		if quality == model.QualityLow {
			args = append(args, "-crf", "28", "-preset", "fast", "-c:a", "aac", "-b:a", "64k")
		} else {
			args = append(args, "-crf", "23", "-preset", "medium", "-c:a", "aac", "-b:a", "128k")
		}
		args = append(args, "-movflags", "+faststart", "-f", "mp4", output)
		cmd = exec.CommandContext(ctx, "ffmpeg", args...)
	} else {
		// Output MP3
		bitrate := "192k"
		if quality == model.QualityLow {
			bitrate = "96k"
		}
		cmd = exec.CommandContext(ctx, "ffmpeg", "-y", "-i", input, "-vn", "-c:a", "libmp3lame", "-b:a", bitrate, "-f", "mp3", output)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("media conversion failed (%w): %s", err, string(out))
	}

	// Extract duration via ffprobe
	durCmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", output)
	if durOut, err := durCmd.Output(); err == nil {
		if durFloat, err := strconv.ParseFloat(strings.TrimSpace(string(durOut)), 64); err == nil {
			m.Duration = int64(durFloat)
		}
	}

	// Extract video thumbnail if format is video and no thumbnail exists yet
	if targetFormat == model.FormatVideo && m.Thumbnail == "" {
		thumbPath := filepath.Join(dir, "thumb.jpg")
		thumbCmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-ss", "00:00:01", "-i", output, "-vframes", "1", "-q:v", "2", thumbPath)
		if err := thumbCmd.Run(); err == nil {
			if tf, err := os.Open(thumbPath); err == nil {
				defer tf.Close()
				thumbStoragePath := "_personal/" + m.ID + "/thumb.jpg"
				if _, err := p.Files.Create(ctx, thumbStoragePath, tf); err == nil {
					m.Thumbnail = "/" + thumbStoragePath
				}
			}
		}
	}

	fi, err := os.Stat(output)
	if err != nil {
		return nil, err
	}
	m.Size = fi.Size()

	f, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	return &convertedFile{File: f, dir: dir}, nil
}
