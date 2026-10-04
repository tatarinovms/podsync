package model

type Type string

const (
	TypeChannel  = Type("channel")
	TypePlaylist = Type("playlist")
	TypeUser     = Type("user")
	TypeGroup    = Type("group")
	TypeHandle   = Type("handle")
)

type Provider string

const (
	ProviderYoutube    = Provider("youtube")
	ProviderVimeo      = Provider("vimeo")
	ProviderSoundcloud = Provider("soundcloud")
	ProviderTwitch     = Provider("twitch")
	ProviderVkVideo    = Provider("vkvideo")
)

// Info represents data extracted from URL
type Info struct {
	LinkType Type     // Either group, channel, user, playlist, or handle
	Provider Provider // Youtube, Vimeo, SoundCloud, Twitch, or VkVideo
	ItemID   string
}
