package ytdl

import (
	"context"
	"encoding/json"
	"fmt"
)

// SingleMetadata resolves an individual URL without importing a playlist.
func (dl *YoutubeDl) SingleMetadata(ctx context.Context, source string) (title, description, thumbnail string, duration int64, err error) {
	args := dl.appendCommonArgs([]string{"--dump-single-json", "--no-playlist", "--skip-download", "--no-warnings"})
	output, err := dl.exec(ctx, append(args, "--", source)...)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("metadata: %s", CleanErrorMessage(output))
	}
	var info struct {
		Type        string  `json:"_type"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Thumbnail   string  `json:"thumbnail"`
		Duration    float64 `json:"duration"`
	}
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		return "", "", "", 0, err
	}
	if info.Type == "playlist" || info.Type == "multi_video" {
		return "", "", "", 0, fmt.Errorf("add an individual video URL, not a playlist")
	}
	return info.Title, info.Description, info.Thumbnail, int64(info.Duration), nil
}
