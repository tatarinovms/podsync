package logbuffer

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Entry represents a structured log event stored in the buffer.
type Entry struct {
	ID        int64                  `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
}

// Filter specifies query criteria for fetching log entries.
type Filter struct {
	Level   string `json:"level"`
	Search  string `json:"search"`
	FeedID  string `json:"feed_id"`
	Limit   int    `json:"limit"`
	SinceID int64  `json:"since_id"`
}

// Result holds the query response along with metadata.
type Result struct {
	Entries       []Entry `json:"entries"`
	Total         int     `json:"total"`
	FilteredTotal int     `json:"filtered_total"`
	MaxID         int64   `json:"max_id"`
	CurrentLevel  string  `json:"current_level"`
}

// Buffer is a thread-safe ring buffer implementing logrus.Hook.
type Buffer struct {
	mu       sync.RWMutex
	capacity int
	entries  []Entry
	nextID   int64
}

// New creates a new Buffer with the given capacity (defaults to 2000).
func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Buffer{
		capacity: capacity,
		entries:  make([]Entry, 0, capacity),
	}
}

// Levels implements logrus.Hook interface.
func (b *Buffer) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire implements logrus.Hook interface.
func (b *Buffer) Fire(entry *logrus.Entry) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++

	var fields map[string]interface{}
	if len(entry.Data) > 0 {
		fields = make(map[string]interface{}, len(entry.Data))
		for k, v := range entry.Data {
			if err, ok := v.(error); ok {
				fields[k] = err.Error()
			} else {
				fields[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	caller := ""
	if entry.Caller != nil {
		caller = fmt.Sprintf("%s:%d", entry.Caller.File, entry.Caller.Line)
	}

	item := Entry{
		ID:        b.nextID,
		Timestamp: entry.Time,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    fields,
		Caller:    caller,
	}

	if len(b.entries) >= b.capacity {
		b.entries = append(b.entries[1:], item)
	} else {
		b.entries = append(b.entries, item)
	}

	return nil
}

// Query returns entries matching the given filter.
func (b *Buffer) Query(f Filter) Result {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := len(b.entries)
	var maxID int64
	if total > 0 {
		maxID = b.entries[total-1].ID
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 200
	} else if limit > 2000 {
		limit = 2000
	}

	searchLower := strings.ToLower(strings.TrimSpace(f.Search))
	levelFilter := strings.ToLower(strings.TrimSpace(f.Level))
	if levelFilter == "" {
		levelFilter = "all"
	}

	var filtered []Entry
	for _, entry := range b.entries {
		if f.SinceID > 0 && entry.ID <= f.SinceID {
			continue
		}

		if levelFilter != "all" {
			switch levelFilter {
			case "error":
				if entry.Level != "error" && entry.Level != "fatal" && entry.Level != "panic" {
					continue
				}
			case "warn", "warning":
				if entry.Level != "error" && entry.Level != "fatal" && entry.Level != "panic" && entry.Level != "warning" {
					continue
				}
			case "info":
				if entry.Level != "info" && entry.Level != "warning" && entry.Level != "error" && entry.Level != "fatal" && entry.Level != "panic" {
					continue
				}
			case "debug":
				// Shows all levels including debug
			default:
				if !strings.EqualFold(entry.Level, levelFilter) {
					continue
				}
			}
		}

		if f.FeedID != "" {
			feedVal, ok := entry.Fields["feed_id"]
			if !ok || !strings.EqualFold(fmt.Sprintf("%v", feedVal), f.FeedID) {
				continue
			}
		}

		if searchLower != "" {
			matched := false
			if strings.Contains(strings.ToLower(entry.Message), searchLower) {
				matched = true
			} else {
				for k, v := range entry.Fields {
					if strings.Contains(strings.ToLower(k), searchLower) ||
						strings.Contains(strings.ToLower(fmt.Sprintf("%v", v)), searchLower) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}

		filtered = append(filtered, entry)
	}

	filteredTotal := len(filtered)
	var resultEntries []Entry
	if len(filtered) > limit {
		resultEntries = filtered[len(filtered)-limit:]
	} else {
		resultEntries = filtered
	}

	return Result{
		Entries:       resultEntries,
		Total:         total,
		FilteredTotal: filteredTotal,
		MaxID:         maxID,
		CurrentLevel:  logrus.GetLevel().String(),
	}
}

// Clear clears all stored entries in the buffer.
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = make([]Entry, 0, b.capacity)
}

// FormatPlainText formats filtered entries into plain text lines.
func (b *Buffer) FormatPlainText(f Filter) string {
	res := b.Query(f)
	var sb strings.Builder
	for _, e := range res.Entries {
		sb.WriteString(e.Timestamp.Format(time.RFC3339))
		sb.WriteString(" [")
		sb.WriteString(strings.ToUpper(e.Level))
		sb.WriteString("] ")
		if len(e.Fields) > 0 {
			sb.WriteString("(")
			first := true
			for k, v := range e.Fields {
				if !first {
					sb.WriteString(" ")
				}
				fmt.Fprintf(&sb, "%s=%v", k, v)
				first = false
			}
			sb.WriteString(") ")
		}
		sb.WriteString(e.Message)
		sb.WriteString("\n")
	}
	return sb.String()
}
