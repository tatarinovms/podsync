package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

var (
	appleIDRegex      = regexp.MustCompile(`(?i)(?:/id|\bid=)(\d+)`)
	appleCountryRegex = regexp.MustCompile(`(?i)apple\.com/([a-zA-Z]{2})/`)
	appleFeedURLRegex = regexp.MustCompile(`"feedUrl"\s*:\s*"([^"]+)"`)
)

func parseApplePodcastsURL(link *url.URL) (model.Type, string, error) {
	fullURL := link.String()
	m := appleIDRegex.FindStringSubmatch(fullURL)
	if len(m) < 2 {
		return "", "", errors.Errorf("could not extract Apple Podcasts show ID from %s", fullURL)
	}
	return model.TypePodcast, m[1], nil
}

type itunesLookupResponse struct {
	ResultCount int `json:"resultCount"`
	Results     []struct {
		CollectionName string `json:"collectionName"`
		ArtistName     string `json:"artistName"`
		FeedURL        string `json:"feedUrl"`
		ArtworkURL600  string `json:"artworkUrl600"`
		ArtworkURL100  string `json:"artworkUrl100"`
		TrackCount     int    `json:"trackCount"`
	} `json:"results"`
}

type ApplePodcastsBuilder struct {
	client     *http.Client
	rssBuilder *RSSBuilder
}

func NewApplePodcastsBuilder() *ApplePodcastsBuilder {
	return NewApplePodcastsBuilderWithHTTPClient(nil)
}

func NewApplePodcastsBuilderWithHTTPClient(client *http.Client) *ApplePodcastsBuilder {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ApplePodcastsBuilder{
		client:     client,
		rssBuilder: NewRSSBuilderWithHTTPClient(client),
	}
}

// ResolveFeedURL discovers the underlying podcast RSS feed URL for an Apple Podcasts URL
func (b *ApplePodcastsBuilder) ResolveFeedURL(ctx context.Context, appleURL string) (string, error) {
	parsed, err := url.Parse(appleURL)
	if err != nil {
		return "", errors.Wrap(err, "invalid Apple Podcasts URL")
	}

	_, id, err := parseApplePodcastsURL(parsed)
	if err != nil {
		return "", err
	}

	country := ""
	if cm := appleCountryRegex.FindStringSubmatch(appleURL); len(cm) >= 2 {
		country = strings.ToLower(cm[1])
	}

	// 1. Try iTunes Lookup API
	feedURL, lookupErr := b.lookupFromAPI(ctx, id, country)
	if lookupErr == nil && feedURL != "" {
		return feedURL, nil
	}

	// 2. Fallback to scraping the Apple Podcasts page HTML
	feedURL, htmlErr := b.lookupFromHTML(ctx, appleURL)
	if htmlErr == nil && feedURL != "" {
		return feedURL, nil
	}

	if lookupErr != nil {
		return "", errors.Wrapf(lookupErr, "failed to resolve Apple podcast %s", id)
	}
	return "", errors.Errorf("no RSS feed URL found for Apple podcast %s", id)
}

func (b *ApplePodcastsBuilder) lookupFromAPI(ctx context.Context, id, country string) (string, error) {
	feedURL, err := b.queryLookupAPI(ctx, id, country)
	if err == nil && feedURL != "" {
		return feedURL, nil
	}

	if country != "" {
		feedURL, err = b.queryLookupAPI(ctx, id, "")
		if err == nil && feedURL != "" {
			return feedURL, nil
		}
	} else {
		feedURL, err = b.queryLookupAPI(ctx, id, "us")
		if err == nil && feedURL != "" {
			return feedURL, nil
		}
	}

	return "", err
}

func (b *ApplePodcastsBuilder) queryLookupAPI(ctx context.Context, id, country string) (string, error) {
	lookupURL := fmt.Sprintf("https://itunes.apple.com/lookup?id=%s&entity=podcast", url.QueryEscape(id))
	if country != "" {
		lookupURL += fmt.Sprintf("&country=%s", url.QueryEscape(country))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultRSSUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.Errorf("iTunes API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var data itunesLookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	if data.ResultCount > 0 && len(data.Results) > 0 {
		feedURL := strings.TrimSpace(data.Results[0].FeedURL)
		if feedURL != "" {
			return feedURL, nil
		}
	}

	return "", errors.New("podcast not found in iTunes store")
}

func (b *ApplePodcastsBuilder) lookupFromHTML(ctx context.Context, appleURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appleURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.Errorf("Apple web page returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}

	m := appleFeedURLRegex.FindSubmatch(body)
	if len(m) >= 2 {
		feedURL := string(m[1])
		feedURL = strings.ReplaceAll(feedURL, `\/`, `/`)
		feedURL = html.UnescapeString(feedURL)
		feedURL = strings.TrimSpace(feedURL)
		if feedURL != "" {
			return feedURL, nil
		}
	}

	return "", errors.New("feedUrl not found in Apple Podcasts page HTML")
}

// FetchFeed downloads and parses RSS metadata for an Apple Podcasts show
func (b *ApplePodcastsBuilder) FetchFeed(ctx context.Context, appleURL string) (*ParsedRSSMetadata, error) {
	feedURL, err := b.ResolveFeedURL(ctx, appleURL)
	if err != nil {
		return nil, err
	}
	return b.rssBuilder.FetchFeed(ctx, feedURL)
}

// Build generates a model.Feed for an Apple Podcasts show by resolving to RSS
func (b *ApplePodcastsBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	feedURL, err := b.ResolveFeedURL(ctx, cfg.URL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve Apple Podcasts URL %s", cfg.URL)
	}

	cloneCfg := *cfg
	cloneCfg.URL = feedURL
	result, err := b.rssBuilder.Build(ctx, &cloneCfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch RSS feed from %s (resolved from %s)", feedURL, cfg.URL)
	}

	result.Provider = model.ProviderApplePodcasts
	result.LinkType = model.TypePodcast
	result.ItemURL = cfg.URL
	return result, nil
}
