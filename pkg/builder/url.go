package builder

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/mxpv/podsync/pkg/model"
)

func ParseURL(link string) (model.Info, error) {
	parsed, err := parseURL(link)
	if err != nil {
		return model.Info{}, err
	}

	info := model.Info{}

	if strings.HasSuffix(parsed.Host, "youtube.com") {
		kind, id, err := parseYoutubeURL(parsed)
		if err != nil {
			return model.Info{}, err
		}

		info.Provider = model.ProviderYoutube
		info.LinkType = kind
		info.ItemID = id

		return info, nil
	}

	if strings.HasSuffix(parsed.Host, "vimeo.com") {
		kind, id, err := parseVimeoURL(parsed)
		if err != nil {
			return model.Info{}, err
		}

		info.Provider = model.ProviderVimeo
		info.LinkType = kind
		info.ItemID = id

		return info, nil
	}

	if strings.HasSuffix(parsed.Host, "soundcloud.com") {
		kind, id, err := parseSoundcloudURL(parsed)
		if err != nil {
			return model.Info{}, err
		}

		info.Provider = model.ProviderSoundcloud
		info.LinkType = kind
		info.ItemID = id

		return info, nil
	}

	if strings.HasSuffix(parsed.Host, "twitch.tv") {
		kind, id, err := parseTwitchURL(parsed)
		if err != nil {
			return model.Info{}, err
		}

		info.Provider = model.ProviderTwitch
		info.LinkType = kind
		info.ItemID = id

		return info, nil
	}

	if strings.HasSuffix(parsed.Host, "vkvideo.ru") || strings.HasSuffix(parsed.Host, "vk.com") || strings.HasSuffix(parsed.Host, "vk.ru") {
		kind, id, err := parseVkURL(parsed)
		if err != nil {
			return model.Info{}, err
		}

		info.Provider = model.ProviderVkVideo
		info.LinkType = kind
		info.ItemID = id

		return info, nil
	}

	return model.Info{}, errors.New("unsupported URL host")
}

func parseURL(link string) (*url.URL, error) {
	if !strings.HasPrefix(link, "http") {
		link = "https://" + link
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse url: %s", link)
	}

	return parsed, nil
}

func parseYoutubeURL(parsed *url.URL) (model.Type, string, error) {
	path := parsed.EscapedPath()

	// https://www.youtube.com/playlist?list=PLCB9F975ECF01953C
	// https://www.youtube.com/watch?v=rbCbho7aLYw&list=PLMpEfaKcGjpWEgNtdnsvLX6LzQL0UC0EM
	if strings.HasPrefix(path, "/playlist") || strings.HasPrefix(path, "/watch") {
		kind := model.TypePlaylist

		id := parsed.Query().Get("list")
		if id != "" {
			return kind, id, nil
		}

		return "", "", errors.New("invalid playlist link")
	}

	// - https://www.youtube.com/channel/UC5XPnUk8Vvv_pWslhwom6Og
	// - https://www.youtube.com/channel/UCrlakW-ewUT8sOod6Wmzyow/videos
	if strings.HasPrefix(path, "/channel") {
		kind := model.TypeChannel
		parts := strings.Split(parsed.EscapedPath(), "/")
		if len(parts) <= 2 {
			return "", "", errors.New("invalid youtube channel link")
		}

		id := parts[2]
		if id == "" {
			return "", "", errors.New("invalid id")
		}

		return kind, id, nil
	}

	// - https://www.youtube.com/user/fxigr1
	if strings.HasPrefix(path, "/user") {
		kind := model.TypeUser

		parts := strings.Split(parsed.EscapedPath(), "/")
		if len(parts) <= 2 {
			return "", "", errors.New("invalid user link")
		}

		id := parts[2]
		if id == "" {
			return "", "", errors.New("invalid id")
		}

		return kind, id, nil
	}

	// - https://www.youtube.com/@username
	// - https://www.youtube.com/@username/videos
	if strings.HasPrefix(path, "/@") {
		kind := model.TypeHandle

		parts := strings.Split(parsed.EscapedPath(), "/")
		if len(parts) <= 1 {
			return "", "", errors.New("invalid handle link")
		}

		handle := parts[1]
		if handle == "" || !strings.HasPrefix(handle, "@") {
			return "", "", errors.New("invalid handle format")
		}

		// Remove the @ prefix for storage
		id := strings.TrimPrefix(handle, "@")
		if id == "" {
			return "", "", errors.New("empty handle")
		}

		return kind, id, nil
	}

	return "", "", errors.New("unsupported link format")
}

func parseVimeoURL(parsed *url.URL) (model.Type, string, error) {
	parts := strings.Split(parsed.EscapedPath(), "/")
	if len(parts) <= 1 {
		return "", "", errors.New("invalid vimeo link path")
	}

	var kind model.Type
	switch parts[1] {
	case "groups":
		kind = model.TypeGroup
	case "channels":
		kind = model.TypeChannel
	default:
		kind = model.TypeUser
	}

	if kind == model.TypeGroup || kind == model.TypeChannel {
		if len(parts) <= 2 {
			return "", "", errors.New("invalid channel link")
		}

		id := parts[2]
		if id == "" {
			return "", "", errors.New("invalid id")
		}

		return kind, id, nil
	}

	if kind == model.TypeUser {
		id := parts[1]
		if id == "" {
			return "", "", errors.New("invalid id")
		}

		return kind, id, nil
	}

	return "", "", errors.New("unsupported link format")
}

func parseSoundcloudURL(parsed *url.URL) (model.Type, string, error) {
	parts := strings.Split(parsed.EscapedPath(), "/")
	if len(parts) <= 3 {
		return "", "", errors.New("invald soundcloud link path")
	}

	var kind model.Type

	// - https://soundcloud.com/user/sets/example-set
	switch parts[2] {
	case "sets":
		kind = model.TypePlaylist
	default:
		return "", "", errors.New("invalid soundcloud url, missing sets")
	}

	id := parts[3]

	return kind, id, nil
}

func parseTwitchURL(parsed *url.URL) (model.Type, string, error) {
	// - https://www.twitch.tv/samueletienne
	path := parsed.EscapedPath()
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return "", "", errors.Errorf("invald twitch user path: %s", path)
	}

	kind := model.TypeUser

	id := parts[1]
	if id == "" {
		return "", "", errors.New("invalid id")
	}

	return kind, id, nil
}

func parseVkURL(parsed *url.URL) (model.Type, string, error) {
	q := parsed.Query()
	section := q.Get("section")
	ownerID := q.Get("owner_id")
	albumID := q.Get("album_id")
	gid := q.Get("gid")

	// 1. Query-based playlist / album
	// e.g. section=playlist_-22822305_456
	if strings.HasPrefix(section, "playlist_") {
		id := strings.TrimPrefix(section, "playlist_")
		if id != "" {
			return model.TypePlaylist, id, nil
		}
	}
	// e.g. section=album_456&owner_id=-22822305
	if strings.HasPrefix(section, "album_") && ownerID != "" {
		alb := strings.TrimPrefix(section, "album_")
		if alb != "" {
			return model.TypePlaylist, fmt.Sprintf("%s_%s", ownerID, alb), nil
		}
	}
	// e.g. owner_id=-22822305&album_id=456
	if ownerID != "" && albumID != "" {
		return model.TypePlaylist, fmt.Sprintf("%s_%s", ownerID, albumID), nil
	}
	// e.g. gid=22822305
	if gid != "" {
		return model.TypeChannel, "-" + strings.TrimPrefix(gid, "-"), nil
	}
	// e.g. section=all&owner_id=-22822305 or just owner_id=-22822305
	if ownerID != "" {
		if strings.HasPrefix(ownerID, "-") {
			return model.TypeChannel, ownerID, nil
		}
		return model.TypeUser, ownerID, nil
	}

	// 2. Path-based parsing
	path := strings.Trim(parsed.EscapedPath(), "/")
	if path == "" {
		return "", "", errors.New("empty vk url path")
	}

	parts := strings.Split(path, "/")

	// Strip trailing tab segments like /videos, /all, /playlists
	for len(parts) > 1 {
		last := parts[len(parts)-1]
		if last == "videos" || last == "all" || last == "playlists" {
			parts = parts[:len(parts)-1]
		} else {
			break
		}
	}

	// Handle /video prefix if present, e.g. /video/playlist/... or /video/@handle or /video/channel/...
	if parts[0] == "video" {
		if len(parts) == 1 {
			return "", "", errors.New("invalid vk video url, missing target")
		}
		parts = parts[1:]
	}

	if len(parts) == 0 {
		return "", "", errors.New("invalid vk url")
	}

	// Case 1: playlist / album -> /playlist/<owner_id>_<album_id> or /album/<owner_id>_<album_id>
	if parts[0] == "playlist" || parts[0] == "album" {
		if len(parts) < 2 || parts[1] == "" {
			return "", "", errors.New("invalid vk playlist url")
		}
		return model.TypePlaylist, parts[1], nil
	}

	// Case 2: channel -> /channel/<id>
	if parts[0] == "channel" {
		if len(parts) < 2 || parts[1] == "" {
			return "", "", errors.New("invalid vk channel url")
		}
		return model.TypeChannel, parts[1], nil
	}

	// Case 3: handle / slug starting with @ -> /@handle
	if strings.HasPrefix(parts[0], "@") {
		handle := strings.TrimPrefix(parts[0], "@")
		if handle == "" {
			return "", "", errors.New("empty vk handle")
		}
		return model.TypeHandle, handle, nil
	}

	// Case 4: user -> /id12345
	if strings.HasPrefix(parts[0], "id") && len(parts[0]) > 2 && isDigits(parts[0][2:]) {
		return model.TypeUser, parts[0][2:], nil
	}

	// Case 5: club / public -> /club12345, /public12345
	if (strings.HasPrefix(parts[0], "club") && len(parts[0]) > 4 && isDigits(parts[0][4:])) ||
		(strings.HasPrefix(parts[0], "public") && len(parts[0]) > 6 && isDigits(parts[0][6:])) {
		return model.TypeGroup, parts[0], nil
	}

	// Case 6: community or user video tab: /videos-12345, /videos12345
	if strings.HasPrefix(parts[0], "videos-") && isDigits(parts[0][7:]) {
		return model.TypeChannel, parts[0][6:], nil // "-12345"
	}
	if strings.HasPrefix(parts[0], "videos") && len(parts[0]) > 6 && isDigits(parts[0][6:]) {
		return model.TypeUser, parts[0][6:], nil // "12345"
	}

	// Case 7: community or user video page: /video-12345, /video12345
	if strings.HasPrefix(parts[0], "video-") && isDigits(parts[0][6:]) {
		return model.TypeChannel, parts[0][5:], nil // "-12345"
	}
	if strings.HasPrefix(parts[0], "video") && len(parts[0]) > 5 && isDigits(parts[0][5:]) {
		return model.TypeUser, parts[0][5:], nil // "12345"
	}

	// Case 8: combined album/playlist slug: /album-22822305_123 or /playlist-22822305_123
	if strings.HasPrefix(parts[0], "album-") && strings.Contains(parts[0], "_") {
		return model.TypePlaylist, "-" + strings.TrimPrefix(parts[0], "album-"), nil
	}
	if strings.HasPrefix(parts[0], "playlist-") && strings.Contains(parts[0], "_") {
		return model.TypePlaylist, "-" + strings.TrimPrefix(parts[0], "playlist-"), nil
	}

	// Case 9: slug/screen_name, e.g. /labelcom
	if parts[0] != "" {
		return model.TypeChannel, parts[0], nil
	}

	return "", "", errors.New("unsupported vk url format")
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
