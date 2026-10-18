package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteConfigTreePreservesModeAndValidTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[feeds.old]\nurl = \"old\"\n"), 0600))
	tree, err := toml.LoadFile(path)
	require.NoError(t, err)
	tree.Set("feeds.new.url", "https://example.com")
	require.NoError(t, writeConfigTree(path, tree))
	loaded, err := toml.LoadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "old", loaded.Get("feeds.old.url"))
	assert.Equal(t, "https://example.com", loaded.Get("feeds.new.url"))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
