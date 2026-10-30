// Package selfupdate installs verified Podsync releases for native deployments.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLatestReleaseURL = "https://api.github.com/repos/tatarinovms/podsync/releases/latest"
	maxArchiveSize          = 512 << 20
)

type platform struct {
	os   string
	arch string
}

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

// Status describes the latest published release and whether it can be installed.
type Status struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseURL      string
	UpdateAvailable bool
	CanUpdate       bool
	Container       bool
	Message         string
}

// Updater downloads release artifacts from GitHub and atomically replaces a
// native Podsync executable after verifying its published checksum.
type Updater struct {
	currentVersion string
	executable     string
	latestURL      string
	client         *http.Client
	platform       platform
	inContainer    func() bool

	mu       sync.Mutex
	cached   release
	cachedAt time.Time
	updateMu sync.Mutex
}

// New creates an updater for executable. Its release checks target the official
// Podsync GitHub repository.
func New(currentVersion, executable string) *Updater {
	return &Updater{
		currentVersion: currentVersion,
		executable:     executable,
		latestURL:      defaultLatestReleaseURL,
		client:         &http.Client{Timeout: 30 * time.Second},
		platform:       platform{os: runtime.GOOS, arch: runtime.GOARCH},
		inContainer:    runningInContainer,
	}
}

// Status returns whether a newer release is available and supported on this host.
func (u *Updater) Status(ctx context.Context) (Status, error) {
	rel, err := u.latestRelease(ctx)
	if err != nil {
		return Status{}, err
	}
	return u.statusForRelease(rel), nil
}

// Apply downloads, verifies, and atomically installs the latest release. The
// caller is responsible for restarting the process after this method succeeds.
func (u *Updater) Apply(ctx context.Context) (Status, error) {
	u.updateMu.Lock()
	defer u.updateMu.Unlock()
	// Do not install a release selected from a stale status check.
	u.mu.Lock()
	u.cachedAt = time.Time{}
	u.mu.Unlock()

	rel, err := u.latestRelease(ctx)
	if err != nil {
		return Status{}, err
	}
	status := u.statusForRelease(rel)
	if !status.CanUpdate {
		return status, fmt.Errorf("Podsync update is unavailable: %s", status.Message)
	}
	if !status.UpdateAvailable {
		return status, fmt.Errorf("Podsync is already running %s", status.CurrentVersion)
	}

	archive, checksum, err := releaseFiles(rel, u.platform)
	if err != nil {
		return status, err
	}
	archiveFile, err := u.downloadVerified(ctx, archive, checksum)
	if err != nil {
		return status, err
	}
	defer os.Remove(archiveFile)

	if err := replaceExecutable(archiveFile, u.executable); err != nil {
		return status, err
	}
	return status, nil
}

func (u *Updater) statusForRelease(rel release) Status {
	status := Status{
		CurrentVersion:  u.currentVersion,
		LatestVersion:   rel.TagName,
		ReleaseURL:      rel.HTMLURL,
		UpdateAvailable: isNewer(rel.TagName, u.currentVersion),
		Container:       u.inContainer(),
	}
	if status.Container {
		status.Message = "Podsync is running in a container. Update the Docker image from the deployment host."
		return status
	}
	if _, _, err := releaseFiles(rel, u.platform); err != nil {
		status.Message = err.Error()
		return status
	}
	if u.executable == "" {
		status.Message = "Podsync executable path is unavailable."
		return status
	}
	if u.platform.os == "windows" {
		status.Message = "Automatic replacement of a running executable is not supported on Windows."
		return status
	}
	status.CanUpdate = true
	if !status.UpdateAvailable {
		status.Message = "Podsync is up to date."
	}
	return status
}

func (u *Updater) latestRelease(ctx context.Context) (release, error) {
	u.mu.Lock()
	if !u.cachedAt.IsZero() && time.Since(u.cachedAt) < 10*time.Minute {
		rel := u.cached
		u.mu.Unlock()
		return rel, nil
	}
	u.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.latestURL, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "podsync-self-update")
	resp, err := u.client.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("request latest Podsync release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("request latest Podsync release: GitHub returned %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("decode latest Podsync release: %w", err)
	}
	if rel.TagName == "" {
		return release{}, fmt.Errorf("latest Podsync release has no version tag")
	}
	u.mu.Lock()
	u.cached = rel
	u.cachedAt = time.Now()
	u.mu.Unlock()
	return rel, nil
}

func releaseFiles(rel release, platform platform) (releaseAsset, releaseAsset, error) {
	if platform.os == "windows" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("automatic updates are unavailable on Windows")
	}
	if platform.os != "linux" && platform.os != "darwin" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("automatic updates are unavailable on %s", platform.os)
	}
	arch, ok := map[string]string{"amd64": "x86_64", "386": "i386", "arm64": "arm64"}[platform.arch]
	if !ok {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("no release archive is available for %s/%s", platform.os, platform.arch)
	}
	prefix := "Podsync_" + strings.TrimPrefix(rel.TagName, "v") + "_" + strings.Title(platform.os) + "_" + arch
	var archive, checksums releaseAsset
	for _, asset := range rel.Assets {
		switch {
		case strings.HasPrefix(asset.Name, prefix) && strings.HasSuffix(asset.Name, ".tar.gz"):
			archive = asset
		case asset.Name == "checksums.txt":
			checksums = asset
		}
	}
	if archive.DownloadURL == "" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("release %s has no archive for %s/%s", rel.TagName, platform.os, platform.arch)
	}
	if checksums.DownloadURL == "" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("release %s has no checksums.txt asset", rel.TagName)
	}
	return archive, checksums, nil
}

func (u *Updater) downloadVerified(ctx context.Context, archive, checksums releaseAsset) (string, error) {
	checksumBody, err := u.download(ctx, checksums.DownloadURL, 4<<20)
	if err != nil {
		return "", fmt.Errorf("download release checksums: %w", err)
	}
	defer checksumBody.Close()
	expected, err := checksumFor(checksumBody, archive.Name)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archive.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download release archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release archive: GitHub returned %s", resp.Status)
	}
	tmp, err := os.CreateTemp("", "podsync-update-*.tar.gz")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	hash := sha256.New()
	_, copyErr := io.Copy(tmp, io.LimitReader(io.TeeReader(resp.Body, hash), maxArchiveSize+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(path)
		if copyErr != nil {
			return "", copyErr
		}
		return "", closeErr
	}
	if info, err := os.Stat(path); err != nil || info.Size() > maxArchiveSize {
		os.Remove(path)
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("release archive exceeds the %d MiB size limit", maxArchiveSize>>20)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, expected) {
		os.Remove(path)
		return "", fmt.Errorf("release archive checksum does not match checksums.txt")
	}
	return path, nil
}

func (u *Updater) download(ctx context.Context, url string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	return readCloser{Reader: io.LimitReader(resp.Body, limit+1), Closer: resp.Body}, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

func checksumFor(r io.Reader, filename string) (string, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			if _, err := hex.DecodeString(fields[0]); err == nil && len(fields[0]) == sha256.Size*2 {
				return fields[0], nil
			}
			return "", fmt.Errorf("invalid SHA-256 checksum for %s", filename)
		}
	}
	return "", fmt.Errorf("checksums.txt does not contain %s", filename)
}

func replaceExecutable(archivePath, executable string) error {
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve Podsync executable: %w", err)
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()

	tmp, err := os.CreateTemp(filepath.Dir(executable), ".podsync-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}

	found := false
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			tmp.Close()
			return fmt.Errorf("read release archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "podsync" {
			continue
		}
		if hdr.Size <= 0 || hdr.Size > maxArchiveSize {
			tmp.Close()
			return fmt.Errorf("invalid Podsync executable in release archive")
		}
		if _, err := io.Copy(tmp, io.LimitReader(tr, hdr.Size)); err != nil {
			tmp.Close()
			return err
		}
		found = true
		break
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("release archive does not contain a Podsync executable")
	}
	if err := os.Rename(tmpPath, executable); err != nil {
		return fmt.Errorf("replace Podsync executable: %w", err)
	}
	return nil
}

func isNewer(latest, current string) bool {
	if strings.TrimPrefix(latest, "v") == strings.TrimPrefix(current, "v") {
		return false
	}
	latestParts, latestOK := numericVersion(latest)
	currentParts, currentOK := numericVersion(current)
	if !latestOK || !currentOK {
		return true
	}
	for i := range latestParts {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return false
}

func numericVersion(version string) ([3]int, bool) {
	var result [3]int
	parts := strings.Split(strings.Split(strings.TrimPrefix(version, "v"), "-")[0], ".")
	if len(parts) != 3 {
		return result, false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return result, false
		}
		result[i] = value
	}
	return result, true
}

func runningInContainer() bool {
	if os.Getenv("container") != "" || os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}
