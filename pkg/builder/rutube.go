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
	"sort"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

const (
	rutubeDefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	rutubeDefaultBaseURL   = "https://rutube.ru/api"
	defaultRutubePageSize  = 50
)

type rutubeAuthor struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	SiteURL   string `json:"site_url"`
}

type rutubeVideoItem struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Description   string       `json:"description"`
	ThumbnailURL  string       `json:"thumbnail_url"`
	VideoURL      string       `json:"video_url"`
	Duration      int64        `json:"duration"`
	CreatedTs     string       `json:"created_ts"`
	PublicationTs string       `json:"publication_ts"`
	Author        rutubeAuthor `json:"author"`
}

type rutubePaginatedResponse struct {
	HasNext  bool               `json:"has_next"`
	Next     string             `json:"next"`
	Page     int                `json:"page"`
	Results  []*rutubeVideoItem `json:"results"`
	NumPages int                `json:"num_pages"`
}

type rutubePlaylistMeta struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	VideosCount  int    `json:"videos_count"`
	ThumbnailURL string `json:"thumbnail_url"`
}

type RutubeBuilder struct {
	client     *http.Client
	downloader Downloader
	userAgent  string
	baseURL    string
	webBaseURL string
}

func NewRutubeBuilder(downloader Downloader) (*RutubeBuilder, error) {
	return &RutubeBuilder{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		downloader: downloader,
		userAgent:  rutubeDefaultUserAgent,
		baseURL:    rutubeDefaultBaseURL,
		webBaseURL: "https://rutube.ru",
	}, nil
}

func parseRutubeDate(str string) (time.Time, error) {
	if str == "" {
		return time.Time{}, errors.New("empty date")
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range formats {
		if t, err := time.Parse(layout, str); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.Errorf("unable to parse rutube date: %s", str)
}

func (b *RutubeBuilder) getJSON(ctx context.Context, reqURL string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", b.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "rutube API request failed for %s", reqURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Errorf("rutube API returned HTTP %d for %s: %s", resp.StatusCode, reqURL, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return errors.Wrapf(err, "failed to decode rutube API JSON from %s", reqURL)
	}

	return nil
}

func (b *RutubeBuilder) resolveSlugToChannelID(ctx context.Context, slug string) (string, error) {
	reqURL := fmt.Sprintf("%s/u/%s/", b.webBaseURL, url.PathEscape(slug))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", b.userAgent)

	resp, err := b.client.Do(req)
	if err != nil {
		return "", errors.Wrapf(err, "failed to fetch rutube user page for slug %q", slug)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("failed to load rutube user page for %q: HTTP %d", slug, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`"channel_id":\s*"?(\d+)"?`)
	if m := re.FindSubmatch(body); len(m) > 1 {
		return string(m[1]), nil
	}

	return "", errors.Errorf("could not resolve channel_id for rutube user %q", slug)
}

func (b *RutubeBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse URL")
	}

	if info.Provider != model.ProviderRutube {
		return nil, errors.Errorf("unexpected provider %q (expected %q)", info.Provider, model.ProviderRutube)
	}

	f := &model.Feed{
		ItemID:       info.ItemID,
		Provider:     model.ProviderRutube,
		LinkType:     info.LinkType,
		Format:       cfg.Format,
		Quality:      cfg.Quality,
		PageSize:     cfg.PageSize,
		PlaylistSort: cfg.PlaylistSort,
		PrivateFeed:  cfg.PrivateFeed,
		UpdatedAt:    time.Now().UTC(),
	}

	switch info.LinkType {
	case model.TypeVideo:
		if err := b.buildVideoFeed(ctx, cfg, f, info.ItemID); err != nil {
			return nil, err
		}
	case model.TypePlaylist:
		if err := b.buildPlaylistFeed(ctx, cfg, f, info.ItemID); err != nil {
			return nil, err
		}
	case model.TypeChannel, model.TypeUser:
		channelID := info.ItemID
		if !isDigits(channelID) {
			resolvedID, err := b.resolveSlugToChannelID(ctx, channelID)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to resolve rutube user %q", channelID)
			}
			channelID = resolvedID
		}
		if err := b.buildChannelFeed(ctx, cfg, f, channelID); err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf("unsupported link type %q for rutube", info.LinkType)
	}

	// Apply custom feed metadata overrides if configured
	if cfg.Custom.Title != "" {
		f.Title = cfg.Custom.Title
	}
	if cfg.Custom.Description != "" {
		f.Description = cfg.Custom.Description
	}
	if cfg.Custom.Author != "" {
		f.Author = cfg.Custom.Author
	}
	if cfg.Custom.CoverArt != "" {
		f.CoverArt = cfg.Custom.CoverArt
	}

	// Apply playlist sorting
	if f.PlaylistSort == model.SortingAsc {
		sort.Slice(f.Episodes, func(i, j int) bool {
			return f.Episodes[i].PubDate.Before(f.Episodes[j].PubDate)
		})
	} else {
		sort.Slice(f.Episodes, func(i, j int) bool {
			return f.Episodes[i].PubDate.After(f.Episodes[j].PubDate)
		})
	}

	if len(f.Episodes) > 0 {
		f.PubDate = f.Episodes[0].PubDate
	} else {
		f.PubDate = time.Now().UTC()
	}

	return f, nil
}

func (b *RutubeBuilder) buildVideoFeed(ctx context.Context, _ *feed.Config, f *model.Feed, videoID string) error {
	reqURL := fmt.Sprintf("%s/video/%s/", b.baseURL, url.PathEscape(videoID))
	var item rutubeVideoItem
	if err := b.getJSON(ctx, reqURL, &item); err != nil {
		return errors.Wrapf(err, "failed to fetch rutube video %q", videoID)
	}

	f.Title = html.UnescapeString(item.Title)
	f.Description = html.UnescapeString(item.Description)
	f.Author = html.UnescapeString(item.Author.Name)
	if f.Author == "" {
		f.Author = "Rutube"
	}
	f.CoverArt = item.ThumbnailURL
	if f.CoverArt == "" && item.Author.AvatarURL != "" {
		f.CoverArt = item.Author.AvatarURL
	}
	f.ItemURL = fmt.Sprintf("https://rutube.ru/video/%s/", item.ID)

	ep := b.videoItemToEpisode(&item)
	f.Episodes = []*model.Episode{ep}
	return nil
}

func (b *RutubeBuilder) buildPlaylistFeed(ctx context.Context, cfg *feed.Config, f *model.Feed, playlistID string) error {
	f.ItemURL = fmt.Sprintf("https://rutube.ru/plst/%s/", playlistID)

	// 1. Fetch playlist metadata
	metaURL := fmt.Sprintf("%s/playlist/custom/%s/", b.baseURL, url.PathEscape(playlistID))
	var meta rutubePlaylistMeta
	if err := b.getJSON(ctx, metaURL, &meta); err != nil {
		log.WithError(err).Warnf("failed to get rutube playlist metadata for %s, continuing with fallback", playlistID)
		f.Title = fmt.Sprintf("Rutube Playlist %s", playlistID)
	} else {
		f.Title = html.UnescapeString(meta.Title)
		f.Description = html.UnescapeString(meta.Description)
		f.CoverArt = meta.ThumbnailURL
	}

	// 2. Fetch playlist videos
	targetCount := cfg.PageSize
	if targetCount <= 0 {
		targetCount = defaultRutubePageSize
	}

	episodes := make([]*model.Episode, 0, targetCount)
	page := 1

	for len(episodes) < targetCount {
		videosURL := fmt.Sprintf("%s/playlist/custom/%s/videos/?page=%d&format=json", b.baseURL, url.PathEscape(playlistID), page)
		var resp rutubePaginatedResponse
		if err := b.getJSON(ctx, videosURL, &resp); err != nil {
			return errors.Wrapf(err, "failed to fetch rutube playlist videos page %d", page)
		}

		if len(resp.Results) == 0 {
			break
		}

		for _, item := range resp.Results {
			ep := b.videoItemToEpisode(item)
			episodes = append(episodes, ep)
			if f.Author == "" && item.Author.Name != "" {
				f.Author = html.UnescapeString(item.Author.Name)
			}
			if f.CoverArt == "" && item.ThumbnailURL != "" {
				f.CoverArt = item.ThumbnailURL
			}
			if len(episodes) >= targetCount {
				break
			}
		}

		if !resp.HasNext || resp.Next == "" {
			break
		}
		page++
	}

	if f.Author == "" {
		f.Author = "Rutube"
	}

	f.Episodes = episodes
	return nil
}

func (b *RutubeBuilder) buildChannelFeed(ctx context.Context, cfg *feed.Config, f *model.Feed, channelID string) error {
	f.ItemURL = fmt.Sprintf("https://rutube.ru/channel/%s/", channelID)

	targetCount := cfg.PageSize
	if targetCount <= 0 {
		targetCount = defaultRutubePageSize
	}

	episodes := make([]*model.Episode, 0, targetCount)
	page := 1

	for len(episodes) < targetCount {
		channelURL := fmt.Sprintf("%s/video/person/%s/?page=%d&format=json", b.baseURL, url.PathEscape(channelID), page)
		var resp rutubePaginatedResponse
		if err := b.getJSON(ctx, channelURL, &resp); err != nil {
			return errors.Wrapf(err, "failed to fetch rutube channel videos page %d", page)
		}

		if len(resp.Results) == 0 {
			break
		}

		for _, item := range resp.Results {
			ep := b.videoItemToEpisode(item)
			episodes = append(episodes, ep)

			if f.Title == "" && item.Author.Name != "" {
				f.Title = html.UnescapeString(item.Author.Name)
			}
			if f.Author == "" && item.Author.Name != "" {
				f.Author = html.UnescapeString(item.Author.Name)
			}
			if f.CoverArt == "" && item.Author.AvatarURL != "" {
				f.CoverArt = item.Author.AvatarURL
			}

			if len(episodes) >= targetCount {
				break
			}
		}

		if !resp.HasNext || resp.Next == "" {
			break
		}
		page++
	}

	if f.Title == "" {
		f.Title = fmt.Sprintf("Rutube Channel %s", channelID)
	}
	if f.Author == "" {
		f.Author = "Rutube"
	}
	if f.CoverArt == "" && len(episodes) > 0 {
		f.CoverArt = episodes[0].Thumbnail
	}

	f.Episodes = episodes
	return nil
}

func (b *RutubeBuilder) videoItemToEpisode(item *rutubeVideoItem) *model.Episode {
	videoURL := item.VideoURL
	if videoURL == "" {
		videoURL = fmt.Sprintf("https://rutube.ru/video/%s/", item.ID)
	}

	dateStr := item.PublicationTs
	if dateStr == "" {
		dateStr = item.CreatedTs
	}

	pubDate, err := parseRutubeDate(dateStr)
	if err != nil {
		pubDate = time.Now().UTC()
	}

	return &model.Episode{
		ID:          item.ID,
		Title:       html.UnescapeString(item.Title),
		Description: html.UnescapeString(item.Description),
		Thumbnail:   item.ThumbnailURL,
		Duration:    item.Duration,
		VideoURL:    videoURL,
		PubDate:     pubDate,
		Status:      model.EpisodeNew,
	}
}
