package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

func createSilentMP3(t *testing.T, path string, title, artist, album string) {
	t.Helper()
	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "anullsrc=r=44100:cl=mono",
		"-t", "1",
	}
	if title != "" {
		args = append(args, "-metadata", "title="+title)
	}
	if artist != "" {
		args = append(args, "-metadata", "artist="+artist)
	}
	if album != "" {
		args = append(args, "-metadata", "album="+album)
	}
	args = append(args, path)

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}

func TestAudiobookBuilder_MultiFileSortAndCover(t *testing.T) {
	tmpDir := t.TempDir()
	bookDir := filepath.Join(tmpDir, "Dune")
	require.NoError(t, os.MkdirAll(bookDir, 0755))

	// Create files out of order: 10, 2, 1
	createSilentMP3(t, filepath.Join(bookDir, "Chapter 10.mp3"), "Chapter 10: Desert", "Frank Herbert", "Dune")
	createSilentMP3(t, filepath.Join(bookDir, "Chapter 2.mp3"), "Chapter 2: Arrakis", "Frank Herbert", "Dune")
	createSilentMP3(t, filepath.Join(bookDir, "Chapter 1.mp3"), "Chapter 1: Caladan", "Frank Herbert", "Dune")

	// Create cover file
	require.NoError(t, os.WriteFile(filepath.Join(bookDir, "cover.jpg"), []byte("dummy-jpg"), 0644))

	b, err := NewAudiobookBuilder(tmpDir)
	require.NoError(t, err)

	cfg := &feed.Config{
		ID:  "dune",
		URL: "audiobook://Dune",
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "Dune", result.Title)
	require.Equal(t, "Frank Herbert", result.Author)
	require.Equal(t, "cover.jpg", result.CoverArt)
	require.Equal(t, model.ProviderAudiobook, result.Provider)
	require.Equal(t, model.TypeAudiobook, result.LinkType)
	require.Len(t, result.Episodes, 3)

	// Verify natural sort order: 1, 2, 10
	require.Equal(t, "Chapter 1.mp3", result.Episodes[0].ID)
	require.Equal(t, "Chapter 1: Caladan", result.Episodes[0].Title)
	require.Equal(t, "1", result.Episodes[0].Order)
	require.Equal(t, model.EpisodeDownloaded, result.Episodes[0].Status)

	require.Equal(t, "Chapter 2.mp3", result.Episodes[1].ID)
	require.Equal(t, "Chapter 2: Arrakis", result.Episodes[1].Title)
	require.Equal(t, "2", result.Episodes[1].Order)

	require.Equal(t, "Chapter 10.mp3", result.Episodes[2].ID)
	require.Equal(t, "Chapter 10: Desert", result.Episodes[2].Title)
	require.Equal(t, "3", result.Episodes[2].Order)

	// Verify sequential pubDates
	require.True(t, result.Episodes[0].PubDate.Before(result.Episodes[1].PubDate))
	require.True(t, result.Episodes[1].PubDate.Before(result.Episodes[2].PubDate))
}

func TestAudiobookBuilder_CustomOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	bookDir := filepath.Join(tmpDir, "Book1")
	require.NoError(t, os.MkdirAll(bookDir, 0755))

	createSilentMP3(t, filepath.Join(bookDir, "track1.mp3"), "Track 1", "Original Author", "Original Album")

	b, err := NewAudiobookBuilder(tmpDir)
	require.NoError(t, err)

	cfg := &feed.Config{
		ID:  "mybook",
		URL: "audiobook://Book1",
		Custom: feed.Custom{
			Title:       "Custom Book Title",
			Author:      "Custom Author",
			Description: "Custom Description",
			CoverArt:    "https://example.com/art.jpg",
		},
	}

	result, err := b.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, "Custom Book Title", result.Title)
	require.Equal(t, "Custom Author", result.Author)
	require.Equal(t, "Custom Description", result.Description)
	require.Equal(t, "https://example.com/art.jpg", result.CoverArt)
}

func TestAudiobookBuilder_EmptyDirectoryError(t *testing.T) {
	tmpDir := t.TempDir()
	emptyDir := filepath.Join(tmpDir, "EmptyBook")
	require.NoError(t, os.MkdirAll(emptyDir, 0755))

	b, err := NewAudiobookBuilder(tmpDir)
	require.NoError(t, err)

	cfg := &feed.Config{
		ID:  "empty",
		URL: "audiobook://EmptyBook",
	}

	_, err = b.Build(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no supported audio files found")
}

func TestAudiobookBuilder_MissingDirectoryError(t *testing.T) {
	b, err := NewAudiobookBuilder("/non/existent/path")
	require.NoError(t, err)

	cfg := &feed.Config{
		ID:  "ghost",
		URL: "audiobook://GhostBook",
	}

	_, err = b.Build(context.Background(), cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "audiobook directory not found")
}
