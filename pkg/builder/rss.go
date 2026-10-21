package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

const (
	defaultRSSUserAgent = "Mozilla/5.0 (compatible; Podsync/2.0; +https://github.com/mxpv/podsync)"
	rssHTTPTimeout      = 30 * time.Second
)

// RSS XML structs for parsing standard Podcast 2.0 / iTunes RSS feeds

type rssFeedXML struct {
	XMLName xml.Name      `xml:"rss"`
	Channel rssChannelXML `xml:"channel"`
}

type rssChannelXML struct {
	Title       string       `xml:"title"`
	Description string       `xml:"description"`
	Link        string       `xml:"link"`
	Language    string       `xml:"language"`
	Author      string       `xml:"author"`
	Image       rssImageXML  `xml:"image"`
	Items       []rssItemXML `xml:"item"`
}

type rssImageXML struct {
	URL  string `xml:"url"`
	Href string `xml:"href,attr"`
}

type rssITunesImgXML struct {
	Href string `xml:"href,attr"`
}

type rssEnclosureXML struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type rssItemXML struct {
	GUID        string          `xml:"guid"`
	Title       string          `xml:"title"`
	Link        string          `xml:"link"`
	Description string          `xml:"description"`
	PubDate     string          `xml:"pubDate"`
	Author      string          `xml:"author"`
	Duration    string          `xml:"duration"`
	Image       rssImageXML     `xml:"image"`
	Enclosure   rssEnclosureXML `xml:"enclosure"`
}

type RSSBuilder struct {
	client *http.Client
}

func NewRSSBuilder() *RSSBuilder {
	return &RSSBuilder{
		client: &http.Client{
			Timeout: rssHTTPTimeout,
		},
	}
}

func (b *RSSBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	parsedFeed, err := b.FetchFeed(ctx, cfg.URL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch RSS feed from %s", cfg.URL)
	}

	result := &model.Feed{
		ID:          cfg.ID,
		Provider:    model.ProviderRSS,
		LinkType:    model.TypeRSS,
		ItemURL:     cfg.URL,
		Title:       cleanXMLText(parsedFeed.Title),
		Description: cleanXMLText(parsedFeed.Description),
		Author:      cleanXMLText(parsedFeed.Author),
		CoverArt:    parsedFeed.CoverArt,
		UpdatedAt:   time.Now().UTC(),
		Format:      cfg.Format,
		Quality:     cfg.Quality,
		PageSize:    cfg.PageSize,
	}

	if result.Format == "" {
		result.Format = model.FormatAudio
	}
	if result.Quality == "" {
		result.Quality = model.QualityHigh
	}

	episodes := make([]*model.Episode, len(parsedFeed.Episodes))
	copy(episodes, parsedFeed.Episodes)

	// Sort episodes based on PlaylistSort
	if cfg.PlaylistSort == model.SortingAsc {
		sort.SliceStable(episodes, func(i, j int) bool {
			if !episodes[i].PubDate.IsZero() && !episodes[j].PubDate.IsZero() {
				return episodes[i].PubDate.Before(episodes[j].PubDate)
			}
			return i > j
		})
	} else {
		// Default: descending (newest first)
		sort.SliceStable(episodes, func(i, j int) bool {
			if !episodes[i].PubDate.IsZero() && !episodes[j].PubDate.IsZero() {
				return episodes[i].PubDate.After(episodes[j].PubDate)
			}
			return i < j
		})
	}

	if cfg.PageSize > 0 && len(episodes) > cfg.PageSize {
		episodes = episodes[:cfg.PageSize]
	}

	result.Episodes = episodes

	return result, nil
}

// ParsedRSSMetadata holds channel-level and summary metadata for feed preview
type ParsedRSSMetadata struct {
	Title         string
	Description   string
	Author        string
	Link          string
	CoverArt      string
	TotalEpisodes int
	TotalBytes    int64
	LatestPubDate *time.Time
	Episodes      []*model.Episode
}

// FetchFeed downloads and parses an RSS feed from the given URL
func (b *RSSBuilder) FetchFeed(ctx context.Context, feedURL string) (*ParsedRSSMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("User-Agent", defaultRSSUserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "HTTP request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("server responded with status %d: %s", resp.StatusCode, resp.Status)
	}

	return ParseRSSStream(resp.Body, feedURL)
}

// ParseRSSStream parses an RSS XML document from a stream
func ParseRSSStream(r io.Reader, sourceURL string) (*ParsedRSSMetadata, error) {
	var rss rssFeedXML
	decoder := xml.NewDecoder(r)
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		// Handled gracefully for utf-8, windows-1251, etc.
		return input, nil
	}

	if err := decoder.Decode(&rss); err != nil {
		return nil, errors.Wrap(err, "failed to decode RSS XML")
	}

	ch := rss.Channel
	title := cleanXMLText(ch.Title)
	author := cleanXMLText(ch.Author)
	if author == "" {
		author = title
	}

	coverArt := strings.TrimSpace(ch.Image.Href)
	if coverArt == "" {
		coverArt = strings.TrimSpace(ch.Image.URL)
	}

	meta := &ParsedRSSMetadata{
		Title:       title,
		Description: cleanXMLText(ch.Description),
		Author:      author,
		Link:        strings.TrimSpace(ch.Link),
		CoverArt:    coverArt,
		Episodes:    make([]*model.Episode, 0, len(ch.Items)),
	}

	var latestPubDate time.Time

	for idx, item := range ch.Items {
		enclosureURL := strings.TrimSpace(item.Enclosure.URL)
		if enclosureURL == "" {
			// Some feeds might put media URL in link if no enclosure
			continue
		}

		guid := strings.TrimSpace(item.GUID)
		if guid == "" {
			// Derive deterministic GUID from enclosure URL
			hasher := sha256.New()
			hasher.Write([]byte(enclosureURL))
			guid = hex.EncodeToString(hasher.Sum(nil))[:16]
		}

		itemTitle := cleanXMLText(item.Title)
		if itemTitle == "" {
			itemTitle = fmt.Sprintf("Episode %d", idx+1)
		}

		pubDate := parsePubDate(item.PubDate)
		if !pubDate.IsZero() && pubDate.After(latestPubDate) {
			latestPubDate = pubDate
		}

		durationSec := parseDuration(item.Duration)

		epCover := strings.TrimSpace(item.Image.Href)
		if epCover == "" {
			epCover = strings.TrimSpace(item.Image.URL)
		}
		if epCover == "" {
			epCover = coverArt
		}

		size := item.Enclosure.Length
		if size > 0 {
			meta.TotalBytes += size
		}

		ep := &model.Episode{
			ID:          sanitizeGUID(guid),
			Title:       itemTitle,
			Description: cleanXMLText(item.Description),
			Thumbnail:   epCover,
			Duration:    durationSec,
			VideoURL:    enclosureURL,
			MediaURL:    enclosureURL,
			PubDate:     pubDate,
			Size:        size,
			Order:       strconv.Itoa(idx + 1),
			Status:      model.EpisodeNew,
		}

		meta.Episodes = append(meta.Episodes, ep)
	}

	meta.TotalEpisodes = len(meta.Episodes)
	if !latestPubDate.IsZero() {
		meta.LatestPubDate = &latestPubDate
	}

	return meta, nil
}

// sanitizeGUID normalizes a GUID string to be safe for filenames and DB keys
func sanitizeGUID(guid string) string {
	cleaned := strings.TrimSpace(guid)
	// Replace URL schemes or path slashes with safe underscores
	safeRe := regexp.MustCompile(`[^a-zA-Z0-9_\-\.]+`)
	sanitized := safeRe.ReplaceAllString(cleaned, "_")
	sanitized = strings.Trim(sanitized, "_.-")
	if len(sanitized) > 64 {
		// If too long, hash it to ensure safe filename
		hasher := sha256.New()
		hasher.Write([]byte(guid))
		return hex.EncodeToString(hasher.Sum(nil))[:24]
	}
	if sanitized == "" {
		hasher := sha256.New()
		hasher.Write([]byte(guid))
		return hex.EncodeToString(hasher.Sum(nil))[:16]
	}
	return sanitized
}

func cleanXMLText(s string) string {
	decoded := html.UnescapeString(s)
	// Strip HTML tags
	stripRe := regexp.MustCompile(`<[^>]*>`)
	cleaned := stripRe.ReplaceAllString(decoded, "")
	return strings.TrimSpace(cleaned)
}

// parsePubDate parses dates from various RSS standard formats
func parsePubDate(dateStr string) time.Time {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return time.Time{}
	}

	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	for _, layout := range formats {
		if t, err := time.Parse(layout, dateStr); err == nil {
			return t.UTC()
		}
	}

	log.Debugf("could not parse pubDate: %q", dateStr)
	return time.Time{}
}

// parseDuration parses duration formats like "01:23:45", "83:45", or "5025" into seconds
func parseDuration(durationStr string) int64 {
	durationStr = strings.TrimSpace(durationStr)
	if durationStr == "" {
		return 0
	}

	// Case 1: Pure seconds (e.g., "3600")
	if sec, err := strconv.ParseInt(durationStr, 10, 64); err == nil {
		return sec
	}

	// Case 2: Colon-separated HH:MM:SS or MM:SS
	parts := strings.Split(durationStr, ":")
	if len(parts) == 2 {
		min, _ := strconv.ParseInt(parts[0], 10, 64)
		sec, _ := strconv.ParseInt(parts[1], 10, 64)
		return min*60 + sec
	} else if len(parts) == 3 {
		hr, _ := strconv.ParseInt(parts[0], 10, 64)
		min, _ := strconv.ParseInt(parts[1], 10, 64)
		sec, _ := strconv.ParseInt(parts[2], 10, 64)
		return hr*3600 + min*60 + sec
	}

	return 0
}

// DetectMediaExtension inspects enclosure URL and Content-Type to determine file extension
func DetectMediaExtension(mediaURL, contentType string) string {
	// First inspect URL path
	if parsed, err := url.Parse(mediaURL); err == nil {
		ext := strings.ToLower(filepath.Ext(parsed.Path))
		ext = strings.TrimPrefix(ext, ".")
		switch ext {
		case "mp3", "m4a", "m4b", "aac", "ogg", "flac", "wav", "mp4", "m4v", "mov", "mkv":
			return ext
		}
	}

	// Fallback to Content-Type
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(ct, "audio/mpeg") || strings.Contains(ct, "audio/mp3"):
		return "mp3"
	case strings.Contains(ct, "audio/mp4") || strings.Contains(ct, "audio/x-m4a") || strings.Contains(ct, "audio/m4a"):
		return "m4a"
	case strings.Contains(ct, "audio/aac"):
		return "aac"
	case strings.Contains(ct, "audio/ogg") || strings.Contains(ct, "application/ogg"):
		return "ogg"
	case strings.Contains(ct, "audio/flac"):
		return "flac"
	case strings.Contains(ct, "video/mp4"):
		return "mp4"
	case strings.Contains(ct, "video/quicktime"):
		return "mov"
	default:
		return "mp3" // Safe audio podcast default
	}
}
