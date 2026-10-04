package builder

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/model"
)

func TestParseYoutubeURL_Playlist(t *testing.T) {
	link, _ := url.ParseRequestURI("https://www.youtube.com/playlist?list=PLCB9F975ECF01953C")
	kind, id, err := parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "PLCB9F975ECF01953C", id)

	link, _ = url.ParseRequestURI("https://www.youtube.com/watch?v=rbCbho7aLYw&list=PLMpEfaKcGjpWEgNtdnsvLX6LzQL0UC0EM")
	kind, id, err = parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "PLMpEfaKcGjpWEgNtdnsvLX6LzQL0UC0EM", id)
}

func TestParseYoutubeURL_Channel(t *testing.T) {
	link, _ := url.ParseRequestURI("https://www.youtube.com/channel/UC5XPnUk8Vvv_pWslhwom6Og")
	kind, id, err := parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "UC5XPnUk8Vvv_pWslhwom6Og", id)

	link, _ = url.ParseRequestURI("https://www.youtube.com/channel/UCrlakW-ewUT8sOod6Wmzyow/videos")
	kind, id, err = parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "UCrlakW-ewUT8sOod6Wmzyow", id)
}

func TestParseYoutubeURL_User(t *testing.T) {
	link, _ := url.ParseRequestURI("https://youtube.com/user/fxigr1")
	kind, id, err := parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, kind)
	require.Equal(t, "fxigr1", id)
}

func TestParseYoutubeURL_Handle(t *testing.T) {
	// Test basic handle URL
	link, _ := url.ParseRequestURI("https://www.youtube.com/@username")
	kind, id, err := parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "username", id)

	// Test handle URL with /videos
	link, _ = url.ParseRequestURI("https://youtube.com/@testchannel/videos")
	kind, id, err = parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "testchannel", id)

	// Test handle URL without www
	link, _ = url.ParseRequestURI("https://youtube.com/@myhandle")
	kind, id, err = parseYoutubeURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "myhandle", id)
}

func TestParseYoutubeURL_InvalidLink(t *testing.T) {
	link, _ := url.ParseRequestURI("https://www.youtube.com/user///")
	_, _, err := parseYoutubeURL(link)
	require.Error(t, err)

	link, _ = url.ParseRequestURI("https://www.youtube.com/channel//videos")
	_, _, err = parseYoutubeURL(link)
	require.Error(t, err)

	// Test invalid handle URLs
	link, _ = url.ParseRequestURI("https://www.youtube.com/@")
	_, _, err = parseYoutubeURL(link)
	require.Error(t, err)

	link, _ = url.ParseRequestURI("https://www.youtube.com/")
	_, _, err = parseYoutubeURL(link)
	require.Error(t, err)

	// Test handle without @ symbol
	link, _ = url.ParseRequestURI("https://www.youtube.com/username")
	_, _, err = parseYoutubeURL(link)
	require.Error(t, err)
}

func TestParseVimeoURL_Group(t *testing.T) {
	link, _ := url.ParseRequestURI("https://vimeo.com/groups/109")
	kind, id, err := parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeGroup, kind)
	require.Equal(t, "109", id)

	link, _ = url.ParseRequestURI("http://vimeo.com/groups/109")
	kind, id, err = parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeGroup, kind)
	require.Equal(t, "109", id)

	link, _ = url.ParseRequestURI("http://www.vimeo.com/groups/109")
	kind, id, err = parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeGroup, kind)
	require.Equal(t, "109", id)

	link, _ = url.ParseRequestURI("https://vimeo.com/groups/109/videos/")
	kind, id, err = parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeGroup, kind)
	require.Equal(t, "109", id)
}

func TestParseVimeoURL_Channel(t *testing.T) {
	link, _ := url.ParseRequestURI("https://vimeo.com/channels/staffpicks")
	kind, id, err := parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "staffpicks", id)

	link, _ = url.ParseRequestURI("http://vimeo.com/channels/staffpicks/146224925")
	kind, id, err = parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "staffpicks", id)
}

func TestParseVimeoURL_User(t *testing.T) {
	link, _ := url.ParseRequestURI("https://vimeo.com/awhitelabelproduct")
	kind, id, err := parseVimeoURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, kind)
	require.Equal(t, "awhitelabelproduct", id)
}

func TestParseVimeoURL_InvalidLink(t *testing.T) {
	link, _ := url.ParseRequestURI("http://www.apple.com")
	_, _, err := parseVimeoURL(link)
	require.Error(t, err)

	link, _ = url.ParseRequestURI("http://www.vimeo.com")
	_, _, err = parseVimeoURL(link)
	require.Error(t, err)
}

func TestParseVkURL_Handle(t *testing.T) {
	link, _ := url.ParseRequestURI("https://vkvideo.ru/@labelcom")
	kind, id, err := parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "labelcom", id)

	link, _ = url.ParseRequestURI("https://vkvideo.ru/@labelcom/videos")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "labelcom", id)

	link, _ = url.ParseRequestURI("https://vkvideo.ru/@labelcom/all")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "labelcom", id)

	link, _ = url.ParseRequestURI("https://vkvideo.ru/@labelcom/playlists")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "labelcom", id)

	link, _ = url.ParseRequestURI("https://vk.com/video/@labelcom")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "labelcom", id)

	link, _ = url.ParseRequestURI("https://vk.ru/@labelcom")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeHandle, kind)
	require.Equal(t, "labelcom", id)
}

func TestParseVkURL_Playlist(t *testing.T) {
	link, _ := url.ParseRequestURI("https://vkvideo.ru/playlist/-22822305_456")
	kind, id, err := parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)

	link, _ = url.ParseRequestURI("https://vk.com/video/playlist/-22822305_456")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)

	link, _ = url.ParseRequestURI("https://vk.com/video?section=playlist_-22822305_456")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)

	link, _ = url.ParseRequestURI("https://vk.com/video?section=album_456&owner_id=-22822305")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)

	link, _ = url.ParseRequestURI("https://vkvideo.ru/playlist?owner_id=-22822305&album_id=456")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)

	link, _ = url.ParseRequestURI("https://vk.com/album-22822305_456")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)

	link, _ = url.ParseRequestURI("https://vk.com/playlist-22822305_456")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypePlaylist, kind)
	require.Equal(t, "-22822305_456", id)
}

func TestParseVkURL_ChannelAndUsers(t *testing.T) {
	link, _ := url.ParseRequestURI("https://vkvideo.ru/channel/12345")
	kind, id, err := parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "12345", id)

	link, _ = url.ParseRequestURI("https://vkvideo.ru/channel/12345/videos")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "12345", id)

	link, _ = url.ParseRequestURI("https://vk.com/video?section=all&owner_id=-22822305")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "-22822305", id)

	link, _ = url.ParseRequestURI("https://vk.com/video?gid=22822305")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "-22822305", id)

	link, _ = url.ParseRequestURI("https://vk.com/club12345")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeGroup, kind)
	require.Equal(t, "club12345", id)

	link, _ = url.ParseRequestURI("https://vk.com/public12345")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeGroup, kind)
	require.Equal(t, "public12345", id)

	link, _ = url.ParseRequestURI("https://vk.com/id12345")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, kind)
	require.Equal(t, "12345", id)

	link, _ = url.ParseRequestURI("https://vk.com/videos-22822305")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "-22822305", id)

	link, _ = url.ParseRequestURI("https://vk.com/videos12345")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, kind)
	require.Equal(t, "12345", id)

	link, _ = url.ParseRequestURI("https://vk.com/video-22822305")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "-22822305", id)

	link, _ = url.ParseRequestURI("https://vk.com/video12345")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeUser, kind)
	require.Equal(t, "12345", id)

	link, _ = url.ParseRequestURI("https://vk.com/labelcom")
	kind, id, err = parseVkURL(link)
	require.NoError(t, err)
	require.Equal(t, model.TypeChannel, kind)
	require.Equal(t, "labelcom", id)
}

func TestParseURL_VkVideo(t *testing.T) {
	info, err := ParseURL("https://vkvideo.ru/@labelcom")
	require.NoError(t, err)
	require.Equal(t, model.ProviderVkVideo, info.Provider)
	require.Equal(t, model.TypeHandle, info.LinkType)
	require.Equal(t, "labelcom", info.ItemID)

	info, err = ParseURL("https://vk.com/video/playlist/-22822305_456")
	require.NoError(t, err)
	require.Equal(t, model.ProviderVkVideo, info.Provider)
	require.Equal(t, model.TypePlaylist, info.LinkType)
	require.Equal(t, "-22822305_456", info.ItemID)

	info, err = ParseURL("https://vk.com/videos-22822305")
	require.NoError(t, err)
	require.Equal(t, model.ProviderVkVideo, info.Provider)
	require.Equal(t, model.TypeChannel, info.LinkType)
	require.Equal(t, "-22822305", info.ItemID)
}
