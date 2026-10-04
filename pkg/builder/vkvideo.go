package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

const (
	defaultVKAPIVersion  = "5.199"
	defaultVKAPIHost     = "https://api.vk.com/method/"
	vkRateLimitErrorCode = 6
	maxAPIRetries        = 3
)

type vkImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type vkGroup struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ScreenName  string `json:"screen_name"`
	Description string `json:"description"`
	Photo200    string `json:"photo_200"`
	Photo100    string `json:"photo_100"`
	Photo50     string `json:"photo_50"`
}

func (g *vkGroup) coverArt() string {
	if g.Photo200 != "" {
		return g.Photo200
	}
	if g.Photo100 != "" {
		return g.Photo100
	}
	return g.Photo50
}

type vkUser struct {
	ID         int64  `json:"id"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	ScreenName string `json:"screen_name"`
	About      string `json:"about"`
	Photo200   string `json:"photo_200"`
	Photo100   string `json:"photo_100"`
	Photo50    string `json:"photo_50"`
}

func (u *vkUser) fullName() string {
	if u.FirstName != "" && u.LastName != "" {
		return fmt.Sprintf("%s %s", u.FirstName, u.LastName)
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return u.LastName
}

func (u *vkUser) coverArt() string {
	if u.Photo200 != "" {
		return u.Photo200
	}
	if u.Photo100 != "" {
		return u.Photo100
	}
	return u.Photo50
}

type vkAlbum struct {
	ID          int64     `json:"id"`
	OwnerID     int64     `json:"owner_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Count       int       `json:"count"`
	UpdatedTime int64     `json:"updated_time"`
	Image       []vkImage `json:"image"`
}

type vkVideo struct {
	ID          int64     `json:"id"`
	OwnerID     int64     `json:"owner_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Duration    int64     `json:"duration"`
	Date        int64     `json:"date"`
	Live        int       `json:"live"`
	Upcoming    int       `json:"upcoming"`
	LiveStatus  string    `json:"live_status"`
	Processing  int       `json:"processing"`
	AccessKey   string    `json:"access_key"`
	Image       []vkImage `json:"image"`
	FirstFrame  []vkImage `json:"first_frame"`
	Photo800    string    `json:"photo_800"`
	Photo320    string    `json:"photo_320"`
	Photo130    string    `json:"photo_130"`
}

func (v *vkVideo) thumbnail() string {
	if u := bestImage(v.Image); u != "" {
		return u
	}
	if u := bestImage(v.FirstFrame); u != "" {
		return u
	}
	if v.Photo800 != "" {
		return v.Photo800
	}
	if v.Photo320 != "" {
		return v.Photo320
	}
	return v.Photo130
}

func bestImage(images []vkImage) string {
	if len(images) == 0 {
		return ""
	}
	best := images[0]
	for _, img := range images[1:] {
		if img.Width > best.Width {
			best = img
		}
	}
	return best.URL
}

type vkVideoList struct {
	Count int        `json:"count"`
	Items []*vkVideo `json:"items"`
}

type vkResolveScreenName struct {
	Type     string `json:"type"`
	ObjectID int64  `json:"object_id"`
}

type vkAPIError struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (e *vkAPIError) Error() string {
	return fmt.Sprintf("VK API error %d: %s", e.ErrorCode, e.ErrorMsg)
}

// vkAPIClient handles HTTP communication with VK API
type vkAPIClient struct {
	httpClient *http.Client
	token      string
	apiURL     string
	apiVersion string
}

func newVKAPIClient(token string) *vkAPIClient {
	return &vkAPIClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		apiURL:     defaultVKAPIHost,
		apiVersion: defaultVKAPIVersion,
	}
}

func (c *vkAPIClient) doRequest(ctx context.Context, method string, params url.Values) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}
	if c.token != "" {
		params.Set("access_token", c.token)
	}
	params.Set("v", c.apiVersion)

	reqURL := fmt.Sprintf("%s%s?%s", c.apiURL, method, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create http request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "request to %s failed", method)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	}

	return body, nil
}

func (c *vkAPIClient) call(ctx context.Context, method string, params url.Values) ([]byte, error) {
	for attempt := 0; attempt < maxAPIRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 1100 * time.Millisecond):
			}
		}

		body, err := c.doRequest(ctx, method, params)
		if err != nil {
			return nil, err
		}

		var errCheck struct {
			Error *vkAPIError `json:"error"`
		}
		if err := json.Unmarshal(body, &errCheck); err == nil && errCheck.Error != nil {
			if errCheck.Error.ErrorCode == vkRateLimitErrorCode && attempt < maxAPIRetries-1 {
				log.Warnf("VK API rate limit hit (error 6), retrying in %ds...", attempt+1)
				continue
			}
			return nil, errCheck.Error
		}

		return body, nil
	}

	return nil, errors.New("exceeded maximum retries for VK API request")
}

func (c *vkAPIClient) resolveScreenName(ctx context.Context, screenName string) (*vkResolveScreenName, error) {
	body, err := c.call(ctx, "utils.resolveScreenName", url.Values{
		"screen_name": []string{screenName},
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal resolveScreenName response")
	}

	trimmed := strings.TrimSpace(string(res.Response))
	if trimmed == "[]" || trimmed == "null" || trimmed == "" {
		return nil, errors.Errorf("screen name %q not found", screenName)
	}

	var result vkResolveScreenName
	if err := json.Unmarshal(res.Response, &result); err != nil {
		return nil, errors.Wrap(err, "failed to parse resolveScreenName object")
	}

	return &result, nil
}

func (c *vkAPIClient) getGroup(ctx context.Context, groupID string) (*vkGroup, error) {
	body, err := c.call(ctx, "groups.getById", url.Values{
		"group_id": []string{groupID},
		"fields":   []string{"description,photo_200,photo_100,photo_50,screen_name"},
	})
	if err != nil {
		return nil, err
	}

	// VK API groups.getById can return either []vkGroup or { "groups": []vkGroup }
	var arrRes struct {
		Response []*vkGroup `json:"response"`
	}
	if err := json.Unmarshal(body, &arrRes); err == nil && len(arrRes.Response) > 0 {
		return arrRes.Response[0], nil
	}

	var objRes struct {
		Response struct {
			Groups []*vkGroup `json:"groups"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &objRes); err == nil && len(objRes.Response.Groups) > 0 {
		return objRes.Response.Groups[0], nil
	}

	return nil, errors.Errorf("group %q not found", groupID)
}

func (c *vkAPIClient) getUser(ctx context.Context, userID string) (*vkUser, error) {
	body, err := c.call(ctx, "users.get", url.Values{
		"user_ids": []string{userID},
		"fields":   []string{"about,photo_200,photo_100,photo_50,screen_name"},
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Response []*vkUser `json:"response"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal users.get response")
	}

	if len(res.Response) == 0 {
		return nil, errors.Errorf("user %q not found", userID)
	}

	return res.Response[0], nil
}

func (c *vkAPIClient) getAlbum(ctx context.Context, ownerID int64, albumID int64) (*vkAlbum, error) {
	body, err := c.call(ctx, "video.getAlbumById", url.Values{
		"owner_id": []string{strconv.FormatInt(ownerID, 10)},
		"album_id": []string{strconv.FormatInt(albumID, 10)},
		"extended": []string{"1"},
	})
	if err != nil {
		return nil, err
	}

	var res struct {
		Response *vkAlbum `json:"response"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal video.getAlbumById response")
	}

	if res.Response == nil {
		return nil, errors.Errorf("album %d for owner %d not found", albumID, ownerID)
	}

	return res.Response, nil
}

func (c *vkAPIClient) getVideos(ctx context.Context, ownerID int64, albumID int64, count int, offset int) (*vkVideoList, error) {
	params := url.Values{
		"owner_id": []string{strconv.FormatInt(ownerID, 10)},
		"count":    []string{strconv.Itoa(count)},
		"offset":   []string{strconv.Itoa(offset)},
		"extended": []string{"1"},
	}
	if albumID != 0 {
		params.Set("album_id", strconv.FormatInt(albumID, 10))
	}

	body, err := c.call(ctx, "video.get", params)
	if err != nil {
		return nil, err
	}

	var res struct {
		Response *vkVideoList `json:"response"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal video.get response")
	}

	if res.Response == nil {
		return &vkVideoList{}, nil
	}

	return res.Response, nil
}

type VkVideoBuilder struct {
	client     *vkAPIClient
	downloader Downloader
}

func NewVkVideoBuilder(token string, downloader Downloader) (*VkVideoBuilder, error) {
	return &VkVideoBuilder{
		client:     newVKAPIClient(token),
		downloader: downloader,
	}, nil
}

func (b *VkVideoBuilder) Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error) {
	info, err := ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse VK Video URL")
	}

	if b.client.token == "" {
		return nil, errors.New("VK API token is required for VK Video feeds (configure in [tokens] vkvideo or PODSYNC_VKVIDEO_API_KEY)")
	}

	f := &model.Feed{
		ItemID:       info.ItemID,
		Provider:     info.Provider,
		LinkType:     info.LinkType,
		Format:       cfg.Format,
		Quality:      cfg.Quality,
		PageSize:     cfg.PageSize,
		PlaylistSort: cfg.PlaylistSort,
		UpdatedAt:    time.Now().UTC(),
	}

	var (
		ownerID int64
		albumID int64
	)

	if info.LinkType == model.TypePlaylist {
		// Playlist item ID is <owner_id>_<album_id>
		parts := strings.Split(info.ItemID, "_")
		if len(parts) != 2 {
			return nil, errors.Errorf("invalid VK playlist ID format: %q (expected owner_album)", info.ItemID)
		}

		parsedAlbumID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid album id: %s", parts[1])
		}
		albumID = parsedAlbumID

		ownerID, err = b.resolveOwnerID(ctx, parts[0])
		if err != nil {
			return nil, errors.Wrapf(err, "failed to resolve owner %q for playlist", parts[0])
		}

		album, err := b.client.getAlbum(ctx, ownerID, albumID)
		if err != nil {
			log.WithError(err).Warnf("failed to get VK album details, using fallback")
			f.Title = fmt.Sprintf("Playlist %d", albumID)
			f.ItemURL = fmt.Sprintf("https://vkvideo.ru/playlist/%d_%d", ownerID, albumID)
		} else {
			f.Title = html.UnescapeString(album.Title)
			f.Description = html.UnescapeString(album.Description)
			f.CoverArt = bestImage(album.Image)
			f.ItemURL = fmt.Sprintf("https://vkvideo.ru/playlist/%d_%d", ownerID, albumID)
		}

		// Also fetch owner metadata for author and fallback cover art
		b.populateOwnerMetadata(ctx, ownerID, f)
	} else {
		// Channel / Group / User / Handle
		var err error
		ownerID, err = b.resolveOwnerID(ctx, info.ItemID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to resolve VK owner: %s", info.ItemID)
		}

		b.populateOwnerMetadata(ctx, ownerID, f)
	}

	// Fetch videos
	if err := b.fetchVideos(ctx, cfg, f, ownerID, albumID); err != nil {
		return nil, errors.Wrap(err, "failed to fetch videos")
	}

	// Apply playlist sorting
	if f.PlaylistSort == model.SortingAsc {
		for i, j := 0, len(f.Episodes)-1; i < j; i, j = i+1, j-1 {
			f.Episodes[i], f.Episodes[j] = f.Episodes[j], f.Episodes[i]
		}
	}

	if len(f.Episodes) > 0 {
		f.PubDate = f.Episodes[0].PubDate
	} else {
		f.PubDate = time.Now().UTC()
	}

	return f, nil
}

func (b *VkVideoBuilder) resolveOwnerID(ctx context.Context, itemID string) (int64, error) {
	// 1. Direct integer check (e.g. -22822305 or 12345)
	if id, err := strconv.ParseInt(itemID, 10, 64); err == nil {
		return id, nil
	}

	// 2. club12345 / public12345 -> group is negative ID
	if strings.HasPrefix(itemID, "club") {
		num := strings.TrimPrefix(itemID, "club")
		if id, err := strconv.ParseInt(num, 10, 64); err == nil {
			return -id, nil
		}
	}
	if strings.HasPrefix(itemID, "public") {
		num := strings.TrimPrefix(itemID, "public")
		if id, err := strconv.ParseInt(num, 10, 64); err == nil {
			return -id, nil
		}
	}

	// 3. id12345 -> user is positive ID
	if strings.HasPrefix(itemID, "id") {
		num := strings.TrimPrefix(itemID, "id")
		if id, err := strconv.ParseInt(num, 10, 64); err == nil {
			return id, nil
		}
	}

	// 4. Resolve screen name / slug via VK API
	resolved, err := b.client.resolveScreenName(ctx, itemID)
	if err != nil {
		// Fallback: try querying group directly with slug
		if grp, gErr := b.client.getGroup(ctx, itemID); gErr == nil {
			return -grp.ID, nil
		}
		// Or try querying user directly with slug
		if usr, uErr := b.client.getUser(ctx, itemID); uErr == nil {
			return usr.ID, nil
		}
		return 0, err
	}

	switch resolved.Type {
	case "group", "page", "event":
		return -resolved.ObjectID, nil
	case "user":
		return resolved.ObjectID, nil
	default:
		return 0, errors.Errorf("unsupported VK object type: %q", resolved.Type)
	}
}

func (b *VkVideoBuilder) populateOwnerMetadata(ctx context.Context, ownerID int64, f *model.Feed) {
	if ownerID < 0 {
		group, err := b.client.getGroup(ctx, strconv.FormatInt(-ownerID, 10))
		if err != nil {
			log.WithError(err).Warnf("failed to fetch group metadata for %d", -ownerID)
			if f.Title == "" {
				f.Title = fmt.Sprintf("VK Community %d", -ownerID)
			}
			if f.ItemURL == "" {
				f.ItemURL = fmt.Sprintf("https://vk.com/club%d", -ownerID)
			}
			return
		}

		if f.Title == "" {
			f.Title = html.UnescapeString(group.Name)
		}
		if f.Description == "" {
			f.Description = html.UnescapeString(group.Description)
		}
		if f.Author == "" {
			f.Author = html.UnescapeString(group.Name)
		}
		if f.CoverArt == "" {
			f.CoverArt = group.coverArt()
		}
		if f.ItemURL == "" {
			if group.ScreenName != "" {
				f.ItemURL = fmt.Sprintf("https://vkvideo.ru/@%s", group.ScreenName)
			} else {
				f.ItemURL = fmt.Sprintf("https://vk.com/club%d", group.ID)
			}
		}
	} else {
		user, err := b.client.getUser(ctx, strconv.FormatInt(ownerID, 10))
		if err != nil {
			log.WithError(err).Warnf("failed to fetch user metadata for %d", ownerID)
			if f.Title == "" {
				f.Title = fmt.Sprintf("VK User %d", ownerID)
			}
			if f.ItemURL == "" {
				f.ItemURL = fmt.Sprintf("https://vk.com/id%d", ownerID)
			}
			return
		}

		fullName := html.UnescapeString(user.fullName())
		if f.Title == "" {
			f.Title = fullName
		}
		if f.Description == "" {
			f.Description = html.UnescapeString(user.About)
		}
		if f.Author == "" {
			f.Author = fullName
		}
		if f.CoverArt == "" {
			f.CoverArt = user.coverArt()
		}
		if f.ItemURL == "" {
			if user.ScreenName != "" {
				f.ItemURL = fmt.Sprintf("https://vkvideo.ru/@%s", user.ScreenName)
			} else {
				f.ItemURL = fmt.Sprintf("https://vk.com/id%d", user.ID)
			}
		}
	}
}

func (b *VkVideoBuilder) fetchVideos(ctx context.Context, cfg *feed.Config, f *model.Feed, ownerID int64, albumID int64) error {
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = model.DefaultPageSize
	}

	offset := 0
	for len(f.Episodes) < pageSize {
		count := pageSize - len(f.Episodes)
		if count > 100 {
			count = 100
		}

		list, err := b.client.getVideos(ctx, ownerID, albumID, count, offset)
		if err != nil {
			return err
		}

		if len(list.Items) == 0 {
			break
		}

		for _, item := range list.Items {
			// Skip live or scheduled streams
			if item.Upcoming == 1 || (item.Live == 1 && item.LiveStatus != "finished") {
				continue
			}

			// Skip processing videos that are not ready for playback/download
			if item.Processing == 1 {
				continue
			}

			// Skip unavailable or empty duration items
			if item.Duration <= 0 && item.Live == 0 {
				continue
			}

			title := html.UnescapeString(item.Title)
			if title == "" {
				title = fmt.Sprintf("Video %d", item.ID)
			}

			pubDate := time.Unix(item.Date, 0).UTC()
			videoURL := fmt.Sprintf("https://vk.com/video%d_%d", item.OwnerID, item.ID)
			if item.AccessKey != "" {
				videoURL = fmt.Sprintf("%s?access_key=%s", videoURL, item.AccessKey)
			}

			downloadSize := getDownloadSize(item.Duration, cfg.Quality, cfg.Format)

			episode := &model.Episode{
				ID:          fmt.Sprintf("%d_%d", item.OwnerID, item.ID),
				Title:       title,
				Description: html.UnescapeString(item.Description),
				Thumbnail:   item.thumbnail(),
				Duration:    item.Duration,
				VideoURL:    videoURL,
				PubDate:     pubDate,
				Size:        downloadSize,
				Status:      model.EpisodeNew,
			}

			f.Episodes = append(f.Episodes, episode)
			if len(f.Episodes) >= pageSize {
				break
			}
		}

		offset += len(list.Items)
		if list.Count > 0 && offset >= list.Count {
			break
		}
	}

	return nil
}

func getDownloadSize(duration int64, quality model.Quality, format model.Format) int64 {
	switch format {
	case model.FormatAudio:
		if quality == model.QualityHigh {
			return duration * highAudioBytesPerSecond
		}
		return duration * lowAudioBytesPerSecond
	case model.FormatVideo:
		if quality == model.QualityHigh {
			return duration * hdBytesPerSecond
		}
		return duration * ldBytesPerSecond
	default:
		return 0
	}
}
