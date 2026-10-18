package builder

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNaturalLess(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"Chapter 10.mp3", "Chapter 2.mp3", "Chapter 1.mp3", "Chapter 20.mp3"},
			expected: []string{"Chapter 1.mp3", "Chapter 2.mp3", "Chapter 10.mp3", "Chapter 20.mp3"},
		},
		{
			input:    []string{"track_1", "track_02", "track_10", "track_2"},
			expected: []string{"track_1", "track_2", "track_02", "track_10"},
		},
		{
			input:    []string{"01_Intro.mp3", "03_Part.mp3", "02_Chapter.mp3"},
			expected: []string{"01_Intro.mp3", "02_Chapter.mp3", "03_Part.mp3"},
		},
		{
			input:    []string{"file100.m4b", "file25.m4b", "file5.m4b", "file1.m4b"},
			expected: []string{"file1.m4b", "file5.m4b", "file25.m4b", "file100.m4b"},
		},
	}

	for _, tt := range tests {
		copied := make([]string, len(tt.input))
		copy(copied, tt.input)
		sort.Slice(copied, func(i, j int) bool {
			return NaturalLess(copied[i], copied[j])
		})
		require.Equal(t, tt.expected, copied)
	}
}
