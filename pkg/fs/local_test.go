package fs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testCtx = context.Background()
)

func TestNewLocal(t *testing.T) {
	local, err := NewLocal("", false, false)
	assert.NoError(t, err)
	assert.NotNil(t, local)
}

func TestLocalWebUIAssets(t *testing.T) {
	oldDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join("..", "..")))
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	stor, err := NewLocal(t.TempDir(), true, false)
	require.NoError(t, err)
	for _, name := range []string{"/index.html", "/admin.css", "/theme.js", "/admin.js"} {
		f, err := stor.Open(name)
		require.NoError(t, err, name)
		info, err := f.Stat()
		require.NoError(t, err, name)
		assert.Greater(t, info.Size(), int64(0), name)
		require.NoError(t, f.Close())
	}
}

func TestLocal_Create(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	assert.NoError(t, err)

	stor, err := NewLocal(tmpDir, false, false)
	assert.NoError(t, err)

	written, err := stor.Create(testCtx, "1/test", bytes.NewBuffer([]byte{1, 5, 7, 8, 3}))
	assert.NoError(t, err)
	assert.EqualValues(t, 5, written)

	stat, err := os.Stat(filepath.Join(tmpDir, "1", "test"))
	assert.NoError(t, err)
	assert.EqualValues(t, 5, stat.Size())
}

func TestLocal_Size(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	assert.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	stor, err := NewLocal(tmpDir, false, false)
	assert.NoError(t, err)

	_, err = stor.Create(testCtx, "1/test", bytes.NewBuffer([]byte{1, 5, 7, 8, 3}))
	assert.NoError(t, err)

	sz, err := stor.Size(testCtx, "1/test")
	assert.NoError(t, err)
	assert.EqualValues(t, 5, sz)
}

func TestLocal_NoSize(t *testing.T) {
	stor, err := NewLocal("", false, false)
	assert.NoError(t, err)

	_, err = stor.Size(testCtx, "1/test")
	assert.True(t, os.IsNotExist(err))
}

func TestLocal_Delete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	assert.NoError(t, err)

	stor, err := NewLocal(tmpDir, false, false)
	assert.NoError(t, err)

	_, err = stor.Create(testCtx, "1/test", bytes.NewBuffer([]byte{1, 5, 7, 8, 3}))
	assert.NoError(t, err)

	err = stor.Delete(testCtx, "1/test")
	assert.NoError(t, err)

	_, err = stor.Size(testCtx, "1/test")
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(filepath.Join(tmpDir, "1", "test"))
	assert.True(t, os.IsNotExist(err))

	err = stor.Delete(testCtx, "1/test")
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestLocal_copyFile(t *testing.T) {
	reader := bytes.NewReader([]byte{1, 2, 4})
	tmpDir, err := os.MkdirTemp("", "")
	assert.NoError(t, err)

	file := filepath.Join(tmpDir, "1")

	l := &Local{}
	size, err := l.copyFile(reader, file)
	assert.NoError(t, err)
	assert.EqualValues(t, 3, size)

	stat, err := os.Stat(file)
	assert.NoError(t, err)
	assert.EqualValues(t, 3, stat.Size())
}

func TestLocal_NoListing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a file inside a subdirectory
	stor, err := NewLocal(tmpDir, false, true)
	assert.NoError(t, err)

	_, err = stor.Create(testCtx, "feed1/episode.mp3", bytes.NewBuffer([]byte{1, 2, 3}))
	assert.NoError(t, err)

	// Opening a file should work
	file, err := stor.Open("/feed1/episode.mp3")
	assert.NoError(t, err)
	file.Close()

	// Opening a directory should return ErrNotExist when NoListing is enabled
	_, err = stor.Open("/feed1")
	assert.True(t, os.IsNotExist(err))

	// Opening root should also return ErrNotExist
	_, err = stor.Open("/")
	assert.True(t, os.IsNotExist(err))
}

func TestLocal_NoListing_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create storage with NoListing disabled
	stor, err := NewLocal(tmpDir, false, false)
	assert.NoError(t, err)

	_, err = stor.Create(testCtx, "feed1/episode.mp3", bytes.NewBuffer([]byte{1, 2, 3}))
	assert.NoError(t, err)

	// Opening a directory should work when NoListing is disabled
	file, err := stor.Open("/feed1")
	assert.NoError(t, err)
	file.Close()
}

func TestLocal_Mount(t *testing.T) {
	dataDir := t.TempDir()
	audiobookDir := t.TempDir()

	// Write an audio file to external audiobookDir
	chapterFile := filepath.Join(audiobookDir, "01_intro.mp3")
	require.NoError(t, os.WriteFile(chapterFile, []byte("audio-bytes-12345"), 0644))

	stor, err := NewLocal(dataDir, false, false)
	require.NoError(t, err)

	// Mount "dune" to audiobookDir
	stor.Mount("dune", audiobookDir)

	mountDir, ok := stor.GetMount("dune")
	require.True(t, ok)
	require.Equal(t, audiobookDir, mountDir)

	// Test Size via mount
	size, err := stor.Size(testCtx, "dune/01_intro.mp3")
	require.NoError(t, err)
	require.Equal(t, int64(17), size)

	// Test Open via mount
	file, err := stor.Open("/dune/01_intro.mp3")
	require.NoError(t, err)
	defer file.Close()

	stat, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, int64(17), stat.Size())

	// Test Unmount
	stor.Unmount("dune")
	_, ok = stor.GetMount("dune")
	require.False(t, ok)
}
