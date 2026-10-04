package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
)

// HTTPClientManager implements web.AdminManager by proxying calls to a running Podsync HTTP server
type HTTPClientManager struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

// NewHTTPClientManager creates a manager that forwards requests to a running Podsync instance
func NewHTTPClientManager(baseURL, username, password string) *HTTPClientManager {
	baseURL = strings.TrimRight(baseURL, "/")
	return &HTTPClientManager{
		baseURL:  baseURL,
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// IsReachable checks if the remote Podsync server is running and responding to health check
func (c *HTTPClientManager) IsReachable(ctx context.Context) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *HTTPClientManager) doRequest(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.username != "" && c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *HTTPClientManager) ListFeeds(ctx context.Context) ([]web.FeedSummary, error) {
	var result []web.FeedSummary
	err := c.doRequest(ctx, http.MethodGet, "/api/v1/feeds", nil, &result)
	return result, err
}

func (c *HTTPClientManager) GetFeedDetail(ctx context.Context, id string) (*web.FeedSummary, []*model.Episode, error) {
	var res struct {
		Feed     *web.FeedSummary `json:"feed"`
		Episodes []*model.Episode `json:"episodes"`
	}
	err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/feeds/%s", id), nil, &res)
	if err != nil {
		return nil, nil, err
	}
	return res.Feed, res.Episodes, nil
}

func (c *HTTPClientManager) AddFeed(ctx context.Context, cfg *feed.Config) error {
	return c.doRequest(ctx, http.MethodPost, "/api/v1/feeds", cfg, nil)
}

func (c *HTTPClientManager) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	path := fmt.Sprintf("/api/v1/feeds/%s", id)
	if deleteFiles {
		path += "?delete_files=true"
	}
	return c.doRequest(ctx, http.MethodDelete, path, nil, nil)
}

func (c *HTTPClientManager) TriggerUpdate(ctx context.Context, id string) error {
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/feeds/%s/update", id), nil, nil)
}

func (c *HTTPClientManager) RetryEpisode(ctx context.Context, feedID, episodeID string) error {
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/feeds/%s/episodes/%s/retry", feedID, episodeID), nil, nil)
}

func (c *HTTPClientManager) GetTokens(ctx context.Context) []web.TokenInfo {
	var result []web.TokenInfo
	_ = c.doRequest(ctx, http.MethodGet, "/api/v1/tokens", nil, &result)
	return result
}

func (c *HTTPClientManager) UpdateTokens(ctx context.Context, provider string, tokens []string) error {
	payload := map[string]interface{}{
		"provider": provider,
		"tokens":   tokens,
	}
	return c.doRequest(ctx, http.MethodPost, "/api/v1/tokens", payload, nil)
}

func (c *HTTPClientManager) GetSystemStats(ctx context.Context) (*web.SystemStats, error) {
	var result web.SystemStats
	err := c.doRequest(ctx, http.MethodGet, "/api/v1/system", nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}


func (c *HTTPClientManager) GetDownloaderConfig(ctx context.Context) (*web.DownloaderConfigInfo, error) {
	var result web.DownloaderConfigInfo
	err := c.doRequest(ctx, http.MethodGet, "/api/v1/downloader", nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *HTTPClientManager) UpdateDownloaderConfig(ctx context.Context, cfg *web.DownloaderConfigUpdate) error {
	return c.doRequest(ctx, http.MethodPost, "/api/v1/downloader", cfg, nil)
}

func (c *HTTPClientManager) GetCookiesContent(ctx context.Context) (string, error) {
	var result web.CookiesPayload
	err := c.doRequest(ctx, http.MethodGet, "/api/v1/downloader/cookies", nil, &result)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c *HTTPClientManager) UpdateCookiesContent(ctx context.Context, content string) error {
	payload := web.CookiesPayload{Content: content}
	if content == "" {
		return c.doRequest(ctx, http.MethodDelete, "/api/v1/downloader/cookies", nil, nil)
	}
	return c.doRequest(ctx, http.MethodPost, "/api/v1/downloader/cookies", payload, nil)
}

func (c *HTTPClientManager) TestDownloader(ctx context.Context, testURL string) (*web.TestDownloaderResult, error) {
	payload := map[string]string{"url": testURL}
	var result web.TestDownloaderResult
	err := c.doRequest(ctx, http.MethodPost, "/api/v1/downloader/test", payload, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
