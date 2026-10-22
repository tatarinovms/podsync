package ytdl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SingleMetadata resolves an individual URL without importing a playlist.
func (dl *YoutubeDl) SingleMetadata(ctx context.Context, source string) (title, description, thumbnail string, duration int64, err error) {
	args := dl.appendCommonArgs([]string{"--dump-single-json", "--no-playlist", "--skip-download", "--no-warnings"})
	output, err := dl.exec(ctx, append(args, "--", source)...)
	if err != nil {
		cleanMsg := CleanErrorMessage(output)
		if strings.Contains(output, "Sign in to confirm you") || strings.Contains(output, "confirm you’re not a bot") {
			return "", "", "", 0, ErrBotDetection
		}
		if strings.Contains(output, "HTTP Error 429") {
			return "", "", "", 0, ErrTooManyRequests
		}
		return "", "", "", 0, fmt.Errorf("metadata: %s", cleanMsg)
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
