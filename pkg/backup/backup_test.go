package backup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateInspectAndRestorePreservesTargetPaths(t *testing.T) {
	source := t.TempDir()
	config := filepath.Join(source, "config.toml")
	require.NoError(t, os.WriteFile(config, []byte("[storage.local]\ndata_dir = '/source/data'\n[database]\ndir = '/source/db'\n[audiobooks]\ndir = '/source/books'\n[server]\nport = 8080\n[server.admin]\npassword = 'secret'\n[tokens]\nyoutube = 'key'\n"), 0600))
	covers := filepath.Join(source, "covers")
	require.NoError(t, os.MkdirAll(covers, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(covers, "cover.png"), []byte("image"), 0600))
	cookies := filepath.Join(source, "cookies.txt")
	require.NoError(t, os.WriteFile(cookies, []byte("cookie"), 0600))
	var archive bytes.Buffer
	manifest, err := Create(context.Background(), ExportOptions{ConfigPath: config, CoversDir: covers, CookiesPath: cookies, Version: "vtest", IncludeSecrets: true, ExportDatabase: func(w io.Writer) error { _, err := w.Write([]byte("badger")); return err }}, &archive)
	require.NoError(t, err)
	require.Equal(t, 1, manifest.Counts.CustomCovers)
	info, err := Inspect(bytes.NewReader(archive.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "vtest", info.PodsyncVersion)
	target := t.TempDir()
	targetConfig := filepath.Join(target, "config.toml")
	require.NoError(t, os.WriteFile(targetConfig, []byte("[storage.local]\ndata_dir = '/target/data'\n[database]\ndir = '/target/db'\n[audiobooks]\ndir = '/target/books'\n[server]\nport = 9090\n"), 0600))
	var restored []byte
	result, err := Restore(context.Background(), bytes.NewReader(archive.Bytes()), RestoreOptions{ConfigPath: targetConfig, CoversDir: filepath.Join(target, "covers"), CookiesPath: filepath.Join(target, "cookies.txt"), PreserveStoragePaths: true, RestoreDatabase: true, RestoreCovers: true, RestoreCookies: true, RestoreDatabaseStream: func(r io.Reader) error { restored, _ = io.ReadAll(r); return nil }})
	require.NoError(t, err)
	require.True(t, result.DatabaseRestored)
	require.Equal(t, []byte("badger"), restored)
	got, err := os.ReadFile(targetConfig)
	require.NoError(t, err)
	require.Contains(t, string(got), "/target/data")
	require.Contains(t, string(got), "/target/db")
	require.Contains(t, string(got), "/target/books")
	require.Contains(t, string(got), "port = 9090")
	require.Equal(t, []byte("image"), mustRead(t, filepath.Join(target, "covers", "cover.png")))
	require.Equal(t, []byte("cookie"), mustRead(t, filepath.Join(target, "cookies.txt")))
}

func TestCreateWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(config, []byte("[tokens]\nyoutube='secret'\n[server.admin]\npassword='secret'\n"), 0600))
	var out bytes.Buffer
	_, err := Create(context.Background(), ExportOptions{ConfigPath: config, IncludeSecrets: false, ExportDatabase: func(w io.Writer) error { _, err := w.Write([]byte("db")); return err }}, &out)
	require.NoError(t, err)
	info, err := Inspect(bytes.NewReader(out.Bytes()))
	require.NoError(t, err)
	require.False(t, info.IncludesSecrets)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
