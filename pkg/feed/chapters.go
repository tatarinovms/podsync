package feed

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/mxpv/podsync/pkg/model"
)

// Podcasting 2.0 Chapters JSON structure
type ChaptersDocument struct {
	Version  string           `json:"version"`
	Chapters []*model.Chapter `json:"chapters"`
}

var timestampRegex = regexp.MustCompile(`(?m)(?:^|[\[\(])(?:(?:(\d{1,2}):)?(\d{1,2}):(\d{2}))[\]\)]?\s*[-–—:]?\s*(.+)$`)

// ParseChaptersFromDescription extracts timestamps and chapter titles from a text description.
func ParseChaptersFromDescription(description string) []*model.Chapter {
	if strings.TrimSpace(description) == "" {
		return nil
	}

	matches := timestampRegex.FindAllStringSubmatch(description, -1)
	if len(matches) == 0 {
		return nil
	}

	var chapters []*model.Chapter
	for _, match := range matches {
		if len(match) < 5 {
			continue
		}

		hoursStr := match[1]
		minsStr := match[2]
		secsStr := match[3]
		title := strings.TrimSpace(match[4])

		if title == "" {
			continue
		}

		mins, err := strconv.Atoi(minsStr)
		if err != nil {
			continue
		}
		secs, err := strconv.Atoi(secsStr)
		if err != nil {
			continue
		}

		var hours int
		if hoursStr != "" {
			hours, _ = strconv.Atoi(hoursStr)
		}

		totalSeconds := float64(hours*3600 + mins*60 + secs)

		// Sanity check: chapters should appear in non-decreasing order
		if len(chapters) > 0 && totalSeconds < chapters[len(chapters)-1].StartTime {
			continue
		}

		chapters = append(chapters, &model.Chapter{
			StartTime: totalSeconds,
			Title:     title,
		})
	}

	if len(chapters) < 2 {
		return nil
	}

	return chapters
}

// GenerateChaptersJSON serializes chapters into Podcasting 2.0 JSON specification.
func GenerateChaptersJSON(chapters []*model.Chapter) ([]byte, error) {
	doc := ChaptersDocument{
		Version:  "1.2.0",
		Chapters: chapters,
	}
	return json.MarshalIndent(doc, "", "  ")
}
