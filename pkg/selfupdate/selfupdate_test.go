package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyReplacesVerifiedExecutable(t *testing.T) {
	archive := releaseArchive(t, []byte("new podsync binary"))
	checksum := sha256.Sum256(archive)
	server := releaseServer(t, archive, hex.EncodeToString(checksum[:]))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "podsync")
	require.NoError(t, os.WriteFile(executable, []byte("old binary"), 0755))
	updater := testUpdater(server.URL, executable)

	status, err := updater.Apply(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1.2.0", status.LatestVersion)
	content, err := os.ReadFile(executable)
	require.NoError(t, err)
	require.Equal(t, []byte("new podsync binary"), content)
}

func TestApplyRejectsInvalidChecksum(t *testing.T) {
	archive := releaseArchive(t, []byte("new podsync binary"))
	server := releaseServer(t, archive, stringsOf('0', sha256.Size*2))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "podsync")
	require.NoError(t, os.WriteFile(executable, []byte("old binary"), 0755))
	_, err := testUpdater(server.URL, executable).Apply(context.Background())
	require.ErrorContains(t, err, "checksum")
	content, readErr := os.ReadFile(executable)
	require.NoError(t, readErr)
	require.Equal(t, []byte("old binary"), content)
}

func TestStatusDisablesContainerUpdates(t *testing.T) {
	archive := releaseArchive(t, []byte("new podsync binary"))
	checksum := sha256.Sum256(archive)
	server := releaseServer(t, archive, hex.EncodeToString(checksum[:]))
	defer server.Close()

	updater := testUpdater(server.URL, filepath.Join(t.TempDir(), "podsync"))
	updater.inContainer = func() bool { return true }
	status, err := updater.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.Container)
	require.False(t, status.CanUpdate)
	require.True(t, status.UpdateAvailable)
}

func testUpdater(serverURL, executable string) *Updater {
	updater := New("v1.0.0", executable)
	updater.latestURL = serverURL + "/latest"
	updater.client = &http.Client{}
	updater.platform = platform{os: "linux", arch: "amd64"}
	updater.inContainer = func() bool { return false }
	return updater
}

func releaseServer(t *testing.T, archive []byte, checksum string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			baseURL := "http://" + r.Host
			_, _ = fmt.Fprintf(w, `{"tag_name":"v1.2.0","html_url":"%s/releases/v1.2.0","assets":[{"name":"Podsync_1.2.0_Linux_x86_64.tar.gz","browser_download_url":"%s/archive"},{"name":"checksums.txt","browser_download_url":"%s/checksums"}]}`, baseURL, baseURL, baseURL)
		case "/archive":
			_, _ = w.Write(archive)
		case "/checksums":
			_, _ = fmt.Fprintf(w, "%s  Podsync_1.2.0_Linux_x86_64.tar.gz\n", checksum)
		default:
			http.NotFound(w, r)
		}
	}))
}

func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	gz := gzip.NewWriter(&result)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "podsync", Mode: 0755, Size: int64(len(binary))}))
	_, err := tw.Write(binary)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return result.Bytes()
}

func stringsOf(char rune, count int) string {
	return string(bytes.Repeat([]byte(string(char)), count))
}
