package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/personal"
	"github.com/mxpv/podsync/services/web"
)

// Server implements the Model Context Protocol (MCP) server
type Server struct {
	mgr      web.AdminManager
	personal *personal.Service
}

// NewServer creates a new MCP server backed by web.AdminManager
func NewServer(mgr web.AdminManager) *Server {
	return &Server{
		mgr: mgr,
	}
}

// SetPersonalFeeds attaches personal feeds service to MCP server
func (s *Server) SetPersonalFeeds(service *personal.Service) {
	s.personal = service
}

// HandleMessage parses a JSON-RPC 2.0 message and returns the response bytes
func (s *Server) HandleMessage(ctx context.Context, data []byte) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    CodeParseError,
				Message: fmt.Sprintf("Parse error: %v", err),
			},
		}
		return json.Marshal(resp)
	}

	// Notifications have no ID and do not return responses
	if req.ID == nil && req.Method == "notifications/initialized" {
		return nil, nil
	}

	resp := s.dispatch(ctx, &req)
	return json.Marshal(resp)
}

func (s *Server) dispatch(ctx context.Context, req *JSONRPCRequest) JSONRPCResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		var params InitializeParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		resp.Result = InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools:     &ToolsCapability{ListChanged: false},
				Resources: &ResourcesCapability{Subscribe: false, ListChanged: false},
				Prompts:   &PromptsCapability{ListChanged: false},
			},
			ServerInfo: Implementation{
				Name:    "podsync-mcp",
				Version: "v2.13.9",
			},
			Instructions: "Podsync MCP Server provides tools to manage podcast feeds, monitor downloads, retry episodes, inspect system status, and configure API tokens for YouTube, VK Video, Vimeo, SoundCloud, and Twitch.",
		}

	case "ping":
		resp.Result = map[string]interface{}{}

	case "tools/list":
		resp.Result = ListToolsResult{
			Tools: s.getToolDefinitions(),
		}

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Invalid params: %v", err),
			}
			return resp
		}
		result, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Result = CallToolResult{
				Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
				IsError: true,
			}
		} else {
			resp.Result = result
		}

	case "resources/list":
		resp.Result = ListResourcesResult{
			Resources: []Resource{
				{
					URI:         "podsync://feeds",
					Name:        "Podcast Feeds",
					Description: "List of all configured podcast feeds, episode counts, and disk usage",
					MimeType:    "application/json",
				},
				{
					URI:         "podsync://system",
					Name:        "System Diagnostics",
					Description: "Podsync runtime health, memory, disk capacity, and downloader tools",
					MimeType:    "application/json",
				},
				{
					URI:         "podsync://tokens",
					Name:        "API Tokens",
					Description: "Status and masked keys for YouTube, VK Video, Vimeo, SoundCloud, Twitch",
					MimeType:    "application/json",
				},
			},
		}

	case "resources/read":
		var params ReadResourceParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Invalid params: %v", err),
			}
			return resp
		}
		result, err := s.readResource(ctx, params.URI)
		if err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInternalError,
				Message: err.Error(),
			}
			return resp
		}
		resp.Result = result

	case "prompts/list":
		resp.Result = ListPromptsResult{
			Prompts: []Prompt{
				{
					Name:        "diagnose_podsync",
					Description: "Diagnose system health, failed episode downloads, and API token readiness",
				},
				{
					Name:        "summarize_library",
					Description: "Generate a detailed executive overview of all podcast channels and disk usage",
				},
			},
		}

	case "prompts/get":
		var params GetPromptParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Invalid params: %v", err),
			}
			return resp
		}
		result, err := s.getPrompt(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInternalError,
				Message: err.Error(),
			}
			return resp
		}
		resp.Result = result

	default:
		resp.Error = &JSONRPCError{
			Code:    CodeMethodNotFound,
			Message: fmt.Sprintf("Method %q not supported", req.Method),
		}
	}

	return resp
}

func (s *Server) getToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "list_feeds",
			Description: "List all configured podcast channels/feeds with stats, URLs, and episode counts",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_feed",
			Description: "Get detailed information about a specific podcast feed and its episode list",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Unique ID of the feed (e.g. LABELCOM, XYZ)",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "add_feed",
			Description: "Add a new podcast feed (e.g., YouTube channel/playlist, VK Video group, Vimeo)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"url": {
						Type:        "string",
						Description: "URL of the channel, group, playlist, or user",
					},
					"format": {
						Type:        "string",
						Description: "Format to download: 'audio' (default) or 'video'",
						Enum:        []string{"audio", "video"},
					},
					"quality": {
						Type:        "string",
						Description: "Media quality: 'high' (default) or 'low'",
						Enum:        []string{"high", "low"},
					},
					"page_size": {
						Type:        "integer",
						Description: "Number of recent episodes to fetch/keep (default: 50)",
					},
					"update_period": {
						Type:        "string",
						Description: "Update interval (e.g., '1h', '6h', '12h', or cron expr)",
					},
					"language": {
						Type:        "string",
						Description: "Optional preferred audio/content language code (e.g. 'ru', 'en') for multi-language videos",
					},
					"redownload": {
						Type:        "boolean",
						Description: "Whether to delete existing downloaded episodes and redownload them with new settings/language (automatically true if language changed)",
					},
				},
				Required: []string{"url"},
			},
		},
		{
			Name:        "delete_feed",
			Description: "Delete a podcast feed from configuration and optionally remove downloaded files from disk",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed to delete",
					},
					"delete_files": {
						Type:        "boolean",
						Description: "Whether to permanently delete downloaded media files from disk",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "update_feed",
			Description: "Trigger an immediate update check and download cycle for a specific feed",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed to update immediately",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "update_all_feeds",
			Description: "Trigger an immediate update check and download cycle for all registered podcast feeds",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "update_feed_config",
			Description: "Update only supplied settings for an existing podcast feed; omitted settings are preserved (cron schedule, format, quality, page_size, retention keep_last, max_height, playlist_sort, private_feed)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed to update",
					},
					"cron_schedule": {
						Type:        "string",
						Description: "Optional cron schedule or interval (e.g. '@every 6h', '0 0 * * *')",
					},
					"page_size": {
						Type:        "integer",
						Description: "Optional page size (number of recent episodes to fetch)",
					},
					"quality": {
						Type:        "string",
						Description: "Optional media quality: 'high' or 'low'",
						Enum:        []string{"high", "low"},
					},
					"format": {
						Type:        "string",
						Description: "Optional media format: 'audio' or 'video'",
						Enum:        []string{"audio", "video"},
					},
					"max_height": {
						Type:        "integer",
						Description: "Optional max video height (e.g. 720, 1080)",
					},
					"keep_last": {
						Type:        "integer",
						Description: "Optional retention policy: keep last N episodes on disk (0 = unlimited)",
					},
					"playlist_sort": {
						Type:        "string",
						Description: "Optional playlist sort: 'asc' or 'desc'",
						Enum:        []string{"asc", "desc"},
					},
					"private_feed": {
						Type:        "boolean",
						Description: "Optional private feed flag (block indexers)",
					},
					"language": {
						Type:        "string",
						Description: "Optional preferred audio/content language code (e.g. 'ru', 'en') for multi-language videos",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "retry_episode",
			Description: "Reset error status of a failed podcast episode to trigger a fresh download attempt",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed",
					},
					"episode_id": {
						Type:        "string",
						Description: "ID of the episode to retry",
					},
				},
				Required: []string{"feed_id", "episode_id"},
			},
		},
		{
			Name:        "get_system_stats",
			Description: "Get Podsync instance health, memory usage, disk space (total/used/free), and yt-dlp/ffmpeg presence",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_tokens",
			Description: "Get configured API keys (masked) and origin (ENV vs config.toml) for all providers",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_downloader_config",
			Description: "Get yt-dlp downloader configuration, ffmpeg version, cookies status, and proxy settings",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "update_downloader_config",
			Description: "Update yt-dlp downloader options such as timeout, proxy, or cookies_from_browser",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"timeout": {
						Type:        "integer",
						Description: "Download timeout in minutes",
					},
					"proxy": {
						Type:        "string",
						Description: "Proxy URL (e.g. http://proxy:8080 or socks5://127.0.0.1:1080)",
					},
					"cookies_from_browser": {
						Type:        "string",
						Description: "Browser to extract cookies from (e.g. chrome, firefox, safari, brave)",
					},
					"language": {
						Type:        "string",
						Description: "Preferred audio/content language code (e.g. 'ru', 'en') for multi-language videos and YouTube dubbing",
					},
				},
			},
		},
		{
			Name:        "test_downloader",
			Description: "Test yt-dlp downloader against a video URL to verify cookies and connectivity",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"url": {
						Type:        "string",
						Description: "URL to test (optional, defaults to YouTube sample video)",
					},
				},
			},
		},
		{
			Name:        "update_tokens",
			Description: "Update API tokens for a provider. Supports multiple tokens for automatic round-robin rotation",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"provider": {
						Type:        "string",
						Description: "Provider identifier: 'youtube', 'vkvideo', 'vimeo', 'soundcloud', 'twitch', 'rutube'",
						Enum:        []string{"youtube", "vkvideo", "vimeo", "soundcloud", "twitch", "rutube"},
					},
					"tokens": {
						Type:        "array",
						Description: "Array of API tokens/keys. For YouTube, multiple keys enable automatic quota rotation",
					},
				},
				Required: []string{"provider", "tokens"},
			},
		},
		{
			Name:        "delete_tokens",
			Description: "Delete a specific API token by index or clear all configured API tokens for a provider from config.toml",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"provider": {
						Type:        "string",
						Description: "Provider identifier: 'youtube', 'vkvideo', 'vimeo', 'soundcloud', 'twitch', 'rutube'",
						Enum:        []string{"youtube", "vkvideo", "vimeo", "soundcloud", "twitch", "rutube"},
					},
					"index": {
						Type:        "integer",
						Description: "Optional 0-based index of the specific token to delete. If omitted or negative, all tokens for the provider are removed.",
					},
				},
				Required: []string{"provider"},
			},
		},
		{
			Name:        "get_download_errors",
			Description: "Get list of failed podcast episode downloads across all feeds or for a specific feed, with detailed error causes and diagnostic recommendations",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Optional feed ID to filter errors by (e.g. LABELCOM, XYZ). If omitted, returns errors across all feeds",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of error items to return (default: 20, max: 100)",
					},
				},
			},
		},
		{
			Name:        "get_recent_downloads",
			Description: "Get list of recently downloaded podcast episodes across all feeds or for a specific feed, with file sizes, media URLs, and timestamps",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Optional feed ID to filter downloads by. If omitted, returns recent downloads across all feeds",
					},
					"limit": {
						Type:        "integer",
						Description: "Maximum number of episodes to return (default: 10, max: 100)",
					},
				},
			},
		},
		{
			Name:        "get_sync_status",
			Description: "Get comprehensive synchronization overview: active feeds, last sync times, next cron runs, downloads past 24h, active error counts, and storage space",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_system_logs",
			Description: "Fetch recent system log entries from Podsync in-memory ring buffer, optionally filtered by level (error, warn, info, debug) or search string",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of log entries to retrieve (default: 50, max: 200)",
					},
					"level": {
						Type:        "string",
						Description: "Log level filter: 'all', 'error', 'warn', 'info', 'debug' (default: 'all')",
						Enum:        []string{"all", "error", "warn", "info", "debug"},
					},
					"search": {
						Type:        "string",
						Description: "Optional case-insensitive substring search across log messages and fields (e.g. 'vk', 'error', 'youtube')",
					},
					"feed_id": {
						Type:        "string",
						Description: "Optional filter to match logs for a specific feed ID",
					},
				},
			},
		},
		{
			Name:        "delete_audiobook_dir",
			Description: "Delete an audiobook folder and its audio files from the audiobooks storage on disk (also removes any linked podcast feed)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir_name": {
						Type:        "string",
						Description: "Directory name inside the audiobooks storage folder to delete",
					},
				},
				Required: []string{"dir_name"},
			},
		},
		{
			Name:        "list_audiobook_dirs",
			Description: "List available directories in the audiobooks storage, showing track count, cover status, total size, and whether each is already linked to a podcast feed",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "list_personal_feeds",
			Description: "List all personal curated podcast feeds (title, ID, episode count, and RSS URL)",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_personal_feed",
			Description: "Get details and episode list of a specific personal podcast feed",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Unique ID of the personal feed (e.g. personal-xyz)",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "create_personal_feed",
			Description: "Create a new universal personal podcast feed that can host both audio and video episodes",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"title": {
						Type:        "string",
						Description: "Title for the personal feed",
					},
					"description": {
						Type:        "string",
						Description: "Description for the feed",
					},
					"cover_art": {
						Type:        "string",
						Description: "Optional HTTP/HTTPS URL of cover art",
					},
					"private_feed": {
						Type:        "boolean",
						Description: "Block search engines and public directories (adds <itunes:block>yes</itunes:block>)",
					},
				},
				Required: []string{"title"},
			},
		},
		{
			Name:        "add_personal_episode",
			Description: "Add an episode to a personal podcast feed from a direct media URL (MP3, OGG, MP4...), supported video platform (YouTube, VK...), or local server file path",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Personal feed ID (e.g. personal-xyz)",
					},
					"url": {
						Type:        "string",
						Description: "HTTP/HTTPS URL to media file (MP3, OGG, MP4, FLAC...) or supported video platform (YouTube, VK, Rutube...)",
					},
					"file_path": {
						Type:        "string",
						Description: "Absolute path to a local audio or video file on the server (e.g. /path/to/audio.mp3, /path/to/media.ogg, /path/to/video.mp4)",
					},
					"title": {
						Type:        "string",
						Description: "Optional custom title for the episode",
					},
					"format": {
						Type:        "string",
						Description: "Media format: 'auto' (default, detects audio vs video), 'audio', or 'video'",
						Enum:        []string{"auto", "audio", "video"},
					},
					"quality": {
						Type:        "string",
						Description: "Audio/media quality: 'high' (default) or 'low'",
						Enum:        []string{"high", "low"},
					},
					"max_height": {
						Type:        "integer",
						Description: "Maximum video height in pixels for video downloads (e.g. 480, 720, 1080). 0 or omitted for default/unlimited",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "delete_personal_episode",
			Description: "Remove an episode from a personal podcast feed",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Personal feed ID",
					},
					"episode_id": {
						Type:        "string",
						Description: "Episode ID to remove",
					},
					"delete_files": {
						Type:        "boolean",
						Description: "Whether to delete the media file from disk if unreferenced by other feeds (default: false)",
					},
				},
				Required: []string{"feed_id", "episode_id"},
			},
		},
		{
			Name:        "rename_personal_episode",
			Description: "Rename an episode in a personal podcast feed",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Personal feed ID",
					},
					"episode_id": {
						Type:        "string",
						Description: "Episode ID to rename",
					},
					"title": {
						Type:        "string",
						Description: "New title for the episode",
					},
				},
				Required: []string{"feed_id", "episode_id", "title"},
			},
		},
		{
			Name:        "import_audiobook",
			Description: "Import a local audiobook folder into a serial podcast feed (Podcast 2.0 / Apple Podcasts serial format)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir_name": {
						Type:        "string",
						Description: "Directory name inside the audiobooks storage folder (e.g. 'George Orwell - 1984')",
					},
					"feed_id": {
						Type:        "string",
						Description: "Optional unique feed ID / slug (auto-generated if omitted)",
					},
					"title": {
						Type:        "string",
						Description: "Optional book title override",
					},
					"author": {
						Type:        "string",
						Description: "Optional author name override",
					},
					"description": {
						Type:        "string",
						Description: "Optional book description override",
					},
					"cron_schedule": {
						Type:        "string",
						Description: "Optional cron schedule (defaults to '@every 12h')",
					},
				},
				Required: []string{"dir_name"},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]interface{}) (CallToolResult, error) {
	if s.mgr == nil {
		return CallToolResult{}, fmt.Errorf("feed manager not initialized")
	}

	switch name {
	case "list_personal_feeds":
		if s.personal == nil {
			return CallToolResult{}, fmt.Errorf("personal feeds service is not enabled")
		}
		feeds, err := s.personal.List()
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(feeds, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_personal_feed":
		if s.personal == nil {
			return CallToolResult{}, fmt.Errorf("personal feeds service is not enabled")
		}
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		detail, err := s.personal.Get(strings.TrimSpace(feedID))
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(detail, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "create_personal_feed":
		if s.personal == nil {
			return CallToolResult{}, fmt.Errorf("personal feeds service is not enabled")
		}
		title, ok := args["title"].(string)
		if !ok || strings.TrimSpace(title) == "" {
			return CallToolResult{}, fmt.Errorf("title parameter is required")
		}
		patch := personal.Patch{Title: &title}
		if desc, ok := args["description"].(string); ok && desc != "" {
			patch.Description = &desc
		}
		if cover, ok := args["cover_art"].(string); ok && cover != "" {
			patch.CoverArt = &cover
		}
		if priv, ok := args["private_feed"].(bool); ok {
			patch.PrivateFeed = &priv
		}
		reserved := map[string]bool{}
		if s.mgr != nil {
			if existingFeeds, err := s.mgr.ListFeeds(ctx); err == nil {
				for _, f := range existingFeeds {
					reserved[f.ID] = true
				}
			}
		}
		created, err := s.personal.Create(ctx, patch, reserved)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(created, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "add_personal_episode":
		if s.personal == nil {
			return CallToolResult{}, fmt.Errorf("personal feeds service is not enabled")
		}
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		title, _ := args["title"].(string)
		format, _ := args["format"].(string)
		quality, _ := args["quality"].(string)
		var maxHeight int
		if mh, ok := args["max_height"].(float64); ok {
			maxHeight = int(mh)
		} else if mh, ok := args["max_height"].(int); ok {
			maxHeight = mh
		}
		urlStr, _ := args["url"].(string)
		filePath, _ := args["file_path"].(string)

		urlStr = strings.TrimSpace(urlStr)
		filePath = strings.TrimSpace(filePath)

		if urlStr == "" && filePath == "" {
			return CallToolResult{}, fmt.Errorf("either 'url' or 'file_path' parameter is required")
		}

		var ep *model.Episode
		var err error
		if filePath != "" {
			ep, err = s.personal.AddPath(ctx, strings.TrimSpace(feedID), filePath, title, format)
		} else {
			ep, err = s.personal.AddURL(ctx, strings.TrimSpace(feedID), urlStr, title, format, quality, maxHeight)
		}
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(ep, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "delete_personal_episode":
		if s.personal == nil {
			return CallToolResult{}, fmt.Errorf("personal feeds service is not enabled")
		}
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		epID, ok := args["episode_id"].(string)
		if !ok || strings.TrimSpace(epID) == "" {
			return CallToolResult{}, fmt.Errorf("episode_id parameter is required")
		}
		delFiles, _ := args["delete_files"].(bool)
		err := s.personal.DeleteEpisode(ctx, strings.TrimSpace(feedID), strings.TrimSpace(epID), delFiles)
		if err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Episode %s removed from feed %s", epID, feedID)}},
		}, nil

	case "rename_personal_episode":
		if s.personal == nil {
			return CallToolResult{}, fmt.Errorf("personal feeds service is not enabled")
		}
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		epID, ok := args["episode_id"].(string)
		if !ok || strings.TrimSpace(epID) == "" {
			return CallToolResult{}, fmt.Errorf("episode_id parameter is required")
		}
		title, ok := args["title"].(string)
		if !ok || strings.TrimSpace(title) == "" {
			return CallToolResult{}, fmt.Errorf("title parameter is required")
		}
		ep, err := s.personal.RenameEpisode(ctx, strings.TrimSpace(feedID), strings.TrimSpace(epID), strings.TrimSpace(title))
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.Marshal(ep)
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "delete_audiobook_dir":
		dirName, ok := args["dir_name"].(string)
		if !ok || strings.TrimSpace(dirName) == "" {
			return CallToolResult{}, fmt.Errorf("dir_name parameter is required")
		}
		if err := s.mgr.DeleteAudiobookDir(ctx, strings.TrimSpace(dirName)); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully deleted audiobook directory %q", dirName)}},
		}, nil

	case "list_audiobook_dirs":
		dirs, err := s.mgr.ListAudiobookDirs(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(dirs, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "import_audiobook":
		dirName, ok := args["dir_name"].(string)
		if !ok || strings.TrimSpace(dirName) == "" {
			return CallToolResult{}, fmt.Errorf("dir_name parameter is required")
		}
		req := web.ImportAudiobookRequest{
			DirName: strings.TrimSpace(dirName),
		}
		if fid, ok := args["feed_id"].(string); ok {
			req.ID = strings.TrimSpace(fid)
		}
		if title, ok := args["title"].(string); ok {
			req.Title = strings.TrimSpace(title)
		}
		if author, ok := args["author"].(string); ok {
			req.Author = strings.TrimSpace(author)
		}
		if desc, ok := args["description"].(string); ok {
			req.Description = strings.TrimSpace(desc)
		}
		if cron, ok := args["cron_schedule"].(string); ok {
			req.CronSchedule = strings.TrimSpace(cron)
		}
		cfg, err := s.mgr.ImportAudiobook(ctx, &req)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully imported audiobook as feed %q:\n%s", cfg.ID, string(data))}},
		}, nil

	case "list_feeds":
		feeds, err := s.mgr.ListFeeds(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(feeds, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_feed":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		summary, episodes, err := s.mgr.GetFeedDetail(ctx, feedID)
		if err != nil {
			return CallToolResult{}, err
		}
		res := map[string]interface{}{
			"feed":     summary,
			"episodes": episodes,
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "add_feed":
		urlStr, ok := args["url"].(string)
		if !ok || strings.TrimSpace(urlStr) == "" {
			return CallToolResult{}, fmt.Errorf("url parameter is required")
		}
		cfg := &feed.Config{
			URL:    strings.TrimSpace(urlStr),
			Format: model.FormatAudio,
		}
		if f, ok := args["format"].(string); ok && f != "" {
			cfg.Format = model.Format(f)
		}
		if q, ok := args["quality"].(string); ok && q != "" {
			cfg.Quality = model.Quality(q)
		}
		if ps, ok := args["page_size"].(float64); ok && ps > 0 {
			cfg.PageSize = int(ps)
		}
		if p, ok := args["update_period"].(string); ok && p != "" {
			if d, err := time.ParseDuration(p); err == nil {
				cfg.UpdatePeriod = d
			} else {
				cfg.CronSchedule = p
			}
		}
		if l, ok := args["language"].(string); ok && l != "" {
			cfg.Custom.Language = strings.TrimSpace(l)
		}

		if err := s.mgr.AddFeed(ctx, cfg); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully added feed %q (%s)", cfg.ID, cfg.URL)}},
		}, nil

	case "delete_feed":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		deleteFiles := false
		if df, ok := args["delete_files"].(bool); ok {
			deleteFiles = df
		}
		if err := s.mgr.DeleteFeed(ctx, feedID, deleteFiles); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully deleted feed %q (delete files: %v)", feedID, deleteFiles)}},
		}, nil

	case "update_feed":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		if err := s.mgr.TriggerUpdate(ctx, feedID); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Update triggered for feed %q", feedID)}},
		}, nil

	case "update_all_feeds":
		queued, err := s.mgr.TriggerUpdateAll(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Update triggered for %d feed(s): %s", len(queued), strings.Join(queued, ", "))}},
		}, nil

	case "update_feed_config":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		// Decode the supplied fields as a patch, preserving omitted values.
		payload, err := json.Marshal(args)
		if err != nil {
			return CallToolResult{}, err
		}
		var update web.FeedConfigUpdate
		if err := json.Unmarshal(payload, &update); err != nil {
			return CallToolResult{}, err
		}
		if err := s.mgr.UpdateFeed(ctx, feedID, &update); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully updated configuration for feed %q", feedID)}},
		}, nil

	case "retry_episode":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		epID, ok := args["episode_id"].(string)
		if !ok || strings.TrimSpace(epID) == "" {
			return CallToolResult{}, fmt.Errorf("episode_id parameter is required")
		}
		if err := s.mgr.RetryEpisode(ctx, feedID, epID); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Episode %q reset for retry in feed %q", epID, feedID)}},
		}, nil

	case "get_system_stats":
		stats, err := s.mgr.GetSystemStats(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_tokens":
		tokens := s.mgr.GetTokens(ctx)
		data, _ := json.MarshalIndent(tokens, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "update_tokens":
		provider, ok := args["provider"].(string)
		if !ok || strings.TrimSpace(provider) == "" {
			return CallToolResult{}, fmt.Errorf("provider parameter is required")
		}
		var tokens []string
		if rawSlice, ok := args["tokens"].([]interface{}); ok {
			for _, item := range rawSlice {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					tokens = append(tokens, strings.TrimSpace(s))
				}
			}
		} else if rawStr, ok := args["tokens"].(string); ok {
			tokens = strings.Fields(rawStr)
		}
		if err := s.mgr.UpdateTokens(ctx, provider, tokens); err != nil {
			return CallToolResult{}, err
		}
		if len(tokens) == 0 {
			return CallToolResult{
				Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully cleared tokens for provider %q", provider)}},
			}, nil
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully updated %d token(s) for provider %q", len(tokens), provider)}},
		}, nil

	case "delete_tokens":
		provider, ok := args["provider"].(string)
		if !ok || strings.TrimSpace(provider) == "" {
			return CallToolResult{}, fmt.Errorf("provider parameter is required")
		}
		idx := -1
		if rawIdx, ok := args["index"]; ok && rawIdx != nil {
			switch v := rawIdx.(type) {
			case float64:
				idx = int(v)
			case int:
				idx = v
			}
		}
		if err := s.mgr.DeleteToken(ctx, provider, idx); err != nil {
			return CallToolResult{}, err
		}
		if idx >= 0 {
			return CallToolResult{
				Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully deleted token #%d for provider %q", idx, provider)}},
			}, nil
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully deleted all API tokens for provider %q from configuration", provider)}},
		}, nil

	case "get_download_errors":
		feedID, _ := args["feed_id"].(string)
		limit := 20
		if rawLimit, ok := args["limit"].(float64); ok && rawLimit > 0 {
			limit = int(rawLimit)
		}
		errs, err := s.mgr.GetDownloadErrors(ctx, feedID, limit)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(errs, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_recent_downloads":
		feedID, _ := args["feed_id"].(string)
		limit := 10
		if rawLimit, ok := args["limit"].(float64); ok && rawLimit > 0 {
			limit = int(rawLimit)
		}
		downloads, err := s.mgr.GetRecentDownloads(ctx, feedID, limit)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(downloads, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_sync_status":
		status, err := s.mgr.GetSyncStatus(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_downloader_config":
		cfg, err := s.mgr.GetDownloaderConfig(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "update_downloader_config":
		var update web.DownloaderConfigUpdate
		if rawTimeout, ok := args["timeout"].(float64); ok && rawTimeout > 0 {
			to := int(rawTimeout)
			update.Timeout = &to
		}
		if rawProxy, ok := args["proxy"].(string); ok {
			update.Proxy = &rawProxy
		}
		if rawBrowser, ok := args["cookies_from_browser"].(string); ok {
			update.CookiesFromBrowser = &rawBrowser
		}
		if rawLang, ok := args["language"].(string); ok {
			update.Language = &rawLang
		}
		if rawBinary, ok := args["custom_binary"].(string); ok {
			update.CustomBinary = &rawBinary
		}
		if rawSelfUpdate, ok := args["self_update"].(bool); ok {
			update.SelfUpdate = &rawSelfUpdate
		}
		if err := s.mgr.UpdateDownloaderConfig(ctx, &update); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: "Successfully updated downloader configuration"}},
		}, nil

	case "test_downloader":
		testURL := ""
		if u, ok := args["url"].(string); ok {
			testURL = strings.TrimSpace(u)
		}
		res, err := s.mgr.TestDownloader(ctx, testURL)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_system_logs":
		limit := 50
		if l, ok := args["limit"].(float64); ok && int(l) > 0 {
			limit = int(l)
			if limit > 200 {
				limit = 200
			}
		}
		level := "all"
		if lvl, ok := args["level"].(string); ok && lvl != "" {
			level = lvl
		}
		search := ""
		if s, ok := args["search"].(string); ok {
			search = s
		}
		feedID := ""
		if fid, ok := args["feed_id"].(string); ok {
			feedID = fid
		}
		logsRes, err := s.mgr.GetSystemLogs(ctx, web.LogFilter{
			Level:  level,
			Search: search,
			FeedID: feedID,
			Limit:  limit,
		})
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(logsRes, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	default:
		return CallToolResult{}, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *Server) readResource(ctx context.Context, uri string) (ReadResourceResult, error) {
	if s.mgr == nil {
		return ReadResourceResult{}, fmt.Errorf("feed manager not initialized")
	}

	switch uri {
	case "podsync://feeds":
		feeds, err := s.mgr.ListFeeds(ctx)
		if err != nil {
			return ReadResourceResult{}, err
		}
		data, _ := json.MarshalIndent(feeds, "", "  ")
		return ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	case "podsync://system":
		stats, err := s.mgr.GetSystemStats(ctx)
		if err != nil {
			return ReadResourceResult{}, err
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	case "podsync://tokens":
		tokens := s.mgr.GetTokens(ctx)
		data, _ := json.MarshalIndent(tokens, "", "  ")
		return ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	default:
		return ReadResourceResult{}, fmt.Errorf("unknown resource URI: %s", uri)
	}
}

func (s *Server) getPrompt(ctx context.Context, name string, args map[string]string) (GetPromptResult, error) {
	switch name {
	case "diagnose_podsync":
		stats, _ := s.mgr.GetSystemStats(ctx)
		tokens := s.mgr.GetTokens(ctx)
		feeds, _ := s.mgr.ListFeeds(ctx)

		systemInfo, _ := json.MarshalIndent(stats, "", "  ")
		tokensInfo, _ := json.MarshalIndent(tokens, "", "  ")
		feedsInfo, _ := json.MarshalIndent(feeds, "", "  ")

		text := fmt.Sprintf(`Please analyze the health of this Podsync instance:

### System Metrics:
%s

### API Tokens:
%s

### Configured Feeds:
%s

Please report:
1. Are required dependencies (yt-dlp, ffmpeg) operational?
2. Are any API tokens missing for configured feeds?
3. Are there any feeds with episode download errors?
4. Are disk usage and memory within healthy limits?`, systemInfo, tokensInfo, feedsInfo)

		return GetPromptResult{
			Description: "Diagnose system health and feeds",
			Messages: []PromptMessage{
				{
					Role:    "user",
					Content: ContentItem{Type: "text", Text: text},
				},
			},
		}, nil

	case "summarize_library":
		feeds, _ := s.mgr.ListFeeds(ctx)
		feedsInfo, _ := json.MarshalIndent(feeds, "", "  ")

		text := fmt.Sprintf(`Here is the current list of podcast feeds hosted by Podsync:
%s

Please summarize:
1. Total channels and distribution by provider (YouTube, VK Video, Vimeo, etc.).
2. Total episodes downloaded and total storage used.
3. Recommendations for update frequencies or cleanup policies if applicable.`, feedsInfo)

		return GetPromptResult{
			Description: "Summarize podcast library",
			Messages: []PromptMessage{
				{
					Role:    "user",
					Content: ContentItem{Type: "text", Text: text},
				},
			},
		}, nil

	default:
		return GetPromptResult{}, fmt.Errorf("unknown prompt: %s", name)
	}
}
