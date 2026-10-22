package model

type Type string

const (
	TypeChannel   = Type("channel")
	TypePlaylist  = Type("playlist")
	TypeUser      = Type("user")
	TypeGroup     = Type("group")
	TypeHandle    = Type("handle")
	TypeVideo     = Type("video")
	TypeAudiobook = Type("audiobook")
	TypeRSS       = Type("rss")
)

type Provider string

const (
	ProviderYoutube    = Provider("youtube")
	ProviderVimeo      = Provider("vimeo")
	ProviderSoundcloud = Provider("soundcloud")
	ProviderTwitch     = Provider("twitch")
	ProviderVkVideo    = Provider("vkvideo")
	ProviderRutube     = Provider("rutube")
	ProviderAudiobook  = Provider("audiobook")
	ProviderPersonal   = Provider("personal")
	ProviderRSS        = Provider("rss")
)

// Info represents data extracted from URL
type Info struct {
	LinkType Type     // Either group, channel, user, playlist, handle, or rss
	Provider Provider // Youtube, Vimeo, SoundCloud, Twitch, VkVideo, Audiobook, or RSS
	ItemID   string
}
