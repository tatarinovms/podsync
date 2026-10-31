package feed

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseChaptersFromDescription(t *testing.T) {
	desc := `
Welcome to the podcast!

Timestamps:
00:00 - Introduction & News
02:15 Main Topic: Audio Normalization
15:30 [Deep Dive] Loudnorm filters
1:10:45 Final thoughts & Outro
`

	chapters := ParseChaptersFromDescription(desc)
	require.Len(t, chapters, 4)

	assert.Equal(t, 0.0, chapters[0].StartTime)
	assert.Equal(t, "Introduction & News", chapters[0].Title)

	assert.Equal(t, 135.0, chapters[1].StartTime)
	assert.Equal(t, "Main Topic: Audio Normalization", chapters[1].Title)

	assert.Equal(t, 930.0, chapters[2].StartTime)
	assert.Equal(t, "[Deep Dive] Loudnorm filters", chapters[2].Title)

	assert.Equal(t, 4245.0, chapters[3].StartTime)
	assert.Equal(t, "Final thoughts & Outro", chapters[3].Title)
}

func TestGenerateChaptersJSON(t *testing.T) {
	desc := `
00:00 Intro
01:30 Discussion
`
	chapters := ParseChaptersFromDescription(desc)
	require.Len(t, chapters, 2)

	data, err := GenerateChaptersJSON(chapters)
	require.NoError(t, err)

	var doc ChaptersDocument
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, "1.2.0", doc.Version)
	assert.Len(t, doc.Chapters, 2)
	assert.Equal(t, 0.0, doc.Chapters[0].StartTime)
	assert.Equal(t, "Intro", doc.Chapters[0].Title)
	assert.Equal(t, 90.0, doc.Chapters[1].StartTime)
	assert.Equal(t, "Discussion", doc.Chapters[1].Title)
}
