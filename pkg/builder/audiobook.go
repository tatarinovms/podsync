package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

var defaultSupportedAudiobookExtensions = []string{
	".mp3", ".m4b", ".m4a", ".aac", ".ogg", ".opus", ".flac",
}

var defaultCoverFilenames = []string{
	"cover.jpg", "cover.png", "folder.jpg", "cover.jpeg",
}

type ffprobeStream struct {
	CodecType   string `json:"codec_type"`
	CodecName   string `json:"codec_name"`
	Duration    string `json:"duration"`
	Disposition struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

type ffprobeFormat struct {
	Duration string            `json:"duration"`
	Size     string            `json:"size"`
	Tags     map[string]string `json:"tags"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type AudiobookBuilder struct {
	defaultDir          string
	supportedExtensions []string
	coverFilenames      []string
}

func NewAudiobookBuilder(defaultDir string) (*AudiobookBuilder, error) {
	if defaultDir == "" {
		defaultDir = os.Getenv("PODSYNC_AUDIOBOOKS_DIR")
	}
	if defaultDir == "" {
		defaultDir = "./audiobooks"
	}
	return &AudiobookBuilder{
		defaultDir:          defaultDir,
		supportedExtensions: defaultSupportedAudiobookExtensions,
		coverFilenames:      defaultCoverFilenames,
	}, nil
}

// ResolveBookDirectory determines the filesystem path for an audiobook feed itemID
func ResolveBookDirectory(baseDir string, cfg *feed.Config, itemID string) (string, error) {
	if cfg != nil && cfg.AudiobookDir != "" {
		if stat, err := os.Stat(cfg.AudiobookDir); err == nil && stat.IsDir() {
			return cfg.AudiobookDir, nil
		}
	}

	// Direct path check
	if filepath.IsAbs(itemID) {
		if stat, err := os.Stat(itemID); err == nil && stat.IsDir() {
			return itemID, nil
		}
	}

	// Relative to baseDir
	if baseDir != "" {
		candidate := filepath.Join(baseDir, itemID)
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate, nil
		}
	}

	// Check if itemID exists as-is in working directory
	if stat, err := os.Stat(itemID); err == nil && stat.IsDir() {
		return itemID, nil
	}

	candidate := filepath.Join(baseDir, itemID)
	return "", errors.Errorf("audiobook directory not found for %q (checked %q and %q)", itemID, candidate, itemID)
}

func (b *AudiobookBuilder) resolveBookDirectory(cfg *feed.Config, itemID string) (string, error) {
	baseDir := b.defaultDir
	if cfg != nil && cfg.AudiobookDir != "" {
		baseDir = cfg.AudiobookDir
	}
	return ResolveBookDirectory(baseDir, cfg, itemID)
}

func (b *AudiobookBuilder) isSupportedAudio(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, supported := range b.supportedExtensions {
		if ext == supported {
			return true
		}
	}
	return false
}

func (b *AudiobookBuilder) findCoverFile(dir string) string {
	for _, coverName := range b.coverFilenames {
		coverPath := filepath.Join(dir, coverName)
		if stat, err := os.Stat(coverPath); err == nil && !stat.IsDir() && stat.Size() > 0 {
			return coverName
		}
	}
	return ""
}

func (b *AudiobookBuilder) probeFile(ctx context.Context, path string) (*ffprobeOutput, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrapf(err, "ffprobe execution failed for %s", path)
	}

	var res ffprobeOutput
	if err := json.Unmarshal(output, &res); err != nil {
		return nil, errors.Wrapf(err, "failed to decode ffprobe output for %s", path)
	}

	return &res, nil
}

func (b *AudiobookBuilder) extractCoverArt(ctx context.Context, audioPath string, targetCoverPath string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", audioPath,
		"-an",
		"-vcodec", "copy",
		targetCoverPath,
	)
	return cmd.Run()
}

func (b *AudiobookBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse URL")
	}

	bookDir, err := b.resolveBookDirectory(cfg, info.ItemID)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(bookDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read audiobook directory: %s", bookDir)
	}

	var audioFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && b.isSupportedAudio(e.Name()) {
			audioFiles = append(audioFiles, e)
		}
	}

	if len(audioFiles) == 0 {
		return nil, errors.Errorf("no supported audio files found in %s", bookDir)
	}

	// Natural sort by filename: "Chapter 1", "Chapter 2", ..., "Chapter 10"
	sort.Slice(audioFiles, func(i, j int) bool {
		return NaturalLess(audioFiles[i].Name(), audioFiles[j].Name())
	})

	// Check cover image
	coverFilename := b.findCoverFile(bookDir)

	f := &model.Feed{
		ID:          cfg.ID,
		ItemID:      info.ItemID,
		Provider:    model.ProviderAudiobook,
		LinkType:    model.TypeAudiobook,
		Format:      model.FormatAudio,
		Quality:     model.QualityHigh,
		PageSize:    len(audioFiles),
		ItemURL:     cfg.URL,
		PrivateFeed: cfg.PrivateFeed,
		UpdatedAt:   time.Now().UTC(),
	}

	var (
		detectedAlbum  string
		detectedArtist string
		detectedDesc   string
	)

	// Base timestamp for sequential pubDates
	baseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	episodes := make([]*model.Episode, 0, len(audioFiles))

	for i, entry := range audioFiles {
		filePath := filepath.Join(bookDir, entry.Name())
		fileInfo, _ := entry.Info()
		fileSize := int64(0)
		if fileInfo != nil {
			fileSize = fileInfo.Size()
		}

		duration := int64(0)
		title := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))

		probe, probeErr := b.probeFile(ctx, filePath)
		if probeErr == nil && probe != nil {
			if durFloat, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil && durFloat > 0 {
				duration = int64(durFloat)
			}
			if probe.Format.Tags != nil {
				if tagTitle, ok := probe.Format.Tags["title"]; ok && strings.TrimSpace(tagTitle) != "" {
					title = strings.TrimSpace(tagTitle)
				}
				if detectedAlbum == "" {
					if tagAlbum, ok := probe.Format.Tags["album"]; ok {
						detectedAlbum = strings.TrimSpace(tagAlbum)
					}
				}
				if detectedArtist == "" {
					if tagArtist, ok := probe.Format.Tags["artist"]; ok {
						detectedArtist = strings.TrimSpace(tagArtist)
					} else if tagAlbumArtist, ok := probe.Format.Tags["album_artist"]; ok {
						detectedArtist = strings.TrimSpace(tagAlbumArtist)
					}
				}
				if detectedDesc == "" {
					if tagComment, ok := probe.Format.Tags["comment"]; ok {
						detectedDesc = strings.TrimSpace(tagComment)
					}
				}
			}

			// Try extracting embedded cover if none was found in directory
			if coverFilename == "" {
				for _, st := range probe.Streams {
					if st.Disposition.AttachedPic == 1 {
						extractedName := "cover.jpg"
						if st.CodecName == "png" {
							extractedName = "cover.png"
						}
						targetPath := filepath.Join(bookDir, extractedName)
						if err := b.extractCoverArt(ctx, filePath, targetPath); err == nil {
							coverFilename = extractedName
						}
						break
					}
				}
			}
		}

		// Incremental pubDate: ensures chapters are strictly chronological
		epPubDate := baseDate.Add(time.Duration(i+1) * time.Hour)

		ep := &model.Episode{
			ID:          entry.Name(),
			Title:       title,
			Description: title,
			Duration:    duration,
			VideoURL:    entry.Name(),
			PubDate:     epPubDate,
			Size:        fileSize,
			Order:       strconv.Itoa(i + 1),
			Status:      model.EpisodeDownloaded,
		}

		episodes = append(episodes, ep)
	}

	// Fallback book title and author
	dirBase := filepath.Base(bookDir)
	if detectedAlbum != "" {
		f.Title = detectedAlbum
	} else {
		f.Title = dirBase
	}

	if detectedArtist != "" {
		f.Author = detectedArtist
	} else {
		f.Author = "Audiobook"
	}

	if detectedDesc != "" {
		f.Description = detectedDesc
	} else {
		f.Description = fmt.Sprintf("Audiobook: %s", f.Title)
	}

	if coverFilename != "" {
		f.CoverArt = coverFilename
	}

	// Apply custom overrides from feed.Config
	if cfg.Custom.Title != "" {
		f.Title = cfg.Custom.Title
	}
	if cfg.Custom.Author != "" {
		f.Author = cfg.Custom.Author
	}
	if cfg.Custom.Description != "" {
		f.Description = cfg.Custom.Description
	}
	if cfg.Custom.CoverArt != "" {
		f.CoverArt = cfg.Custom.CoverArt
	}

	f.Episodes = episodes
	if len(episodes) > 0 {
		f.PubDate = episodes[0].PubDate
	} else {
		f.PubDate = time.Now().UTC()
	}

	return f, nil
}
