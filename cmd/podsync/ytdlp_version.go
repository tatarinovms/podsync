package main

// yt-dlp release checks for Web Admin. The installed binary can come from a
// standalone download, a package manager, or a custom path, so the check uses
// the binary's own --version output instead of assuming an installation method.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mxpv/podsync/services/web"
)

var ytDlpLatestReleaseURL = "https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest"

type ytDlpRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func latestYtDlpRelease(ctx context.Context) (tag, releaseURL string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ytDlpLatestReleaseURL, nil)
	if err != nil {
		return "", "", errors.New("failed to check the latest yt-dlp release")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "podsync")
	res, err := client.Do(req)
	if err != nil {
		return "", "", errors.New("failed to check the latest yt-dlp release: GitHub is unreachable")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", "", errors.New("failed to read the latest yt-dlp release")
	}
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to check the latest yt-dlp release: GitHub returned HTTP %d", res.StatusCode)
	}
	var release ytDlpRelease
	if err := json.Unmarshal(body, &release); err != nil || strings.TrimSpace(release.TagName) == "" {
		return "", "", errors.New("failed to read the latest yt-dlp release")
	}
	return strings.TrimSpace(release.TagName), strings.TrimSpace(release.HTMLURL), nil
}

func parseYtDlpVersion(value string) ([3]int, bool) {
	var result [3]int
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return result, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return result, false
		}
		result[i] = number
	}
	return result, true
}

func ytDlpUpdateAvailable(current, latest string) bool {
	current = strings.TrimSpace(strings.TrimPrefix(current, "v"))
	latest = strings.TrimSpace(strings.TrimPrefix(latest, "v"))
	if current == "" {
		return true
	}
	if current == latest {
		return false
	}
	currentParts, currentOK := parseYtDlpVersion(current)
	latestParts, latestOK := parseYtDlpVersion(latest)
	if !currentOK {
		return true
	}
	if !latestOK {
		return false
	}
	for i := range currentParts {
		if currentParts[i] != latestParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return false
}

func (m *AppFeedManager) CheckYtDlpUpdate(ctx context.Context) (*web.YtDlpVersionInfo, error) {
	m.mu.RLock()
	dl := m.downloader
	m.mu.RUnlock()
	if dl == nil {
		return nil, errors.New("yt-dlp is not configured")
	}

	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	current, err := dl.GetVersion(versionCtx)
	cancel()
	if err != nil {
		return nil, errors.New("unable to read the installed yt-dlp version")
	}
	current = strings.TrimSpace(current)

	latest, releaseURL, err := latestYtDlpRelease(ctx)
	if err != nil {
		return nil, err
	}

	cfg := dl.GetConfig()
	available := ytDlpUpdateAvailable(current, latest)
	message := fmt.Sprintf("yt-dlp %s is up to date.", current)
	if available {
		message = fmt.Sprintf("yt-dlp %s is available; installed %s.", latest, current)
		if cfg.SelfUpdate {
			message += " Podsync will update it automatically on its next 24h check or restart."
		} else {
			message += " Automatic updates are disabled for this binary; update it manually or through your package manager."
		}
	}

	return &web.YtDlpVersionInfo{
		CurrentVersion:  current,
		LatestVersion:   latest,
		UpdateAvailable: available,
		ReleaseURL:      releaseURL,
		BinaryPath:      dl.GetBinaryPath(),
		SelfUpdate:      cfg.SelfUpdate,
		Message:         message,
	}, nil
}
