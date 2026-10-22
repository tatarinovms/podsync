package personal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func (p *MediaProcessor) Process(ctx context.Context, m *Media) (io.ReadCloser, error) {
	if m.SourcePath == "" {
		title, description, thumbnail, duration, err := p.Downloader.SingleMetadata(ctx, m.SourceURL)
		if err != nil {
			return nil, err
		}
		if m.Title == "" || m.Title == m.SourceURL {
			m.Title = title
		}
		m.Description = description
		m.Thumbnail = thumbnail
		m.Duration = duration
		cfg := &feed.Config{ID: m.ID, Format: m.Format, Quality: model.QualityHigh, YouTubeDLArgs: []string{"--no-playlist"}}
		if m.Format == model.FormatVideo {
			cfg.YouTubeDLArgs = append(cfg.YouTubeDLArgs, "--remux-video", "mp4")
		}
		return p.Downloader.Download(ctx, cfg, &model.Episode{ID: m.ID, VideoURL: m.SourceURL, Title: m.Title})
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
	_, copyErr := io.Copy(out, source)
	closeErr := out.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	extension := "mp3"
	args := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe", "-i", input}
	if m.Format == model.FormatAudio {
		args = append(args, "-vn", "-c:a", "libmp3lame", "-q:a", "2")
	} else {
		extension = "mp4"
		args = append(args, "-map", "0:v:0", "-map", "0:a?", "-c:v", "libx264", "-preset", "fast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart")
	}
	output := filepath.Join(dir, "media."+extension)
	result, err := exec.CommandContext(ctx, "ffmpeg", append(args, output)...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("media conversion: %s: %w", strings.TrimSpace(string(result)), err)
	}
	probe, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", output).Output()
	if err == nil {
		var data struct {
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if json.Unmarshal(probe, &data) == nil {
			v, _ := strconv.ParseFloat(data.Format.Duration, 64)
			m.Duration = int64(v)
		}
	}
	f, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	success = true
	return &convertedFile{f, dir}, nil
}
