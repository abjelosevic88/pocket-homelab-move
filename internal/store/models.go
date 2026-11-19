package store

import (
	"encoding/json"
	"strings"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// MoveOut is one moving-out archive: a person's data from the apps they picked.
type MoveOut struct {
	ID         int64          `json:"id"`
	UserID     int64          `json:"-"`
	Status     string         `json:"status"` // queued | running | done | failed
	Apps       []string       `json:"apps"`
	Results    []OutAppResult `json:"results"`
	File       string         `json:"-"`
	SizeBytes  int64          `json:"size_bytes"`
	Error      string         `json:"error"`
	CreatedAt  string         `json:"created_at"`
	FinishedAt string         `json:"finished_at"`
	ExpiresAt  string         `json:"expires_at"`
}

// OutAppResult is how one app went in a moving-out archive.
type OutAppResult struct {
	App   string    `json:"app"`
	Name  string    `json:"name"`
	State string    `json:"state"` // waiting | done | failed | skipped
	Files []OutFile `json:"files"`
	Error string    `json:"error,omitempty"`
}

// OutFile is one file in the archive.
type OutFile struct {
	Path   string `json:"path"` // inside the archive
	Kind   string `json:"kind"`
	Title  string `json:"title,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// MoveIn is one file (or archive) a person brought in, and what Move found in it.
type MoveIn struct {
	ID         int64    `json:"id"`
	UserID     int64    `json:"-"`
	Status     string   `json:"status"` // uploading | scanning | ready | running | done | failed
	Name       string   `json:"name"`   // the uploaded file's name
	Source     string   `json:"source"` // google-takeout | evernote | notion | apple | pocket-move | files
	SourceName string   `json:"source_name"`
	File       string   `json:"-"`
	SizeBytes  int64    `json:"size_bytes"`
	Error      string   `json:"error"`
	Notes      []string `json:"notes"` // what was left out and why
	CreatedAt  string   `json:"created_at"`
	FinishedAt string   `json:"finished_at"`
	Items      []Item   `json:"items,omitempty"`
}

// Item is one thing found in a move-in and where it goes.
type Item struct {
	ID       int64             `json:"id"`
	MoveID   int64             `json:"move_id"`
	UserID   int64             `json:"-"`
	Label    string            `json:"label"`  // "Contacts"
	Detail   string            `json:"detail"` // "1,204 contacts from Google Contacts"
	Kind     string            `json:"kind"`   // a Portable kind
	Count    int               `json:"count"`
	File     string            `json:"-"` // the converted file, ready to send
	FileName string            `json:"file_name"`
	Bytes    int64             `json:"bytes"`
	SHA256   string            `json:"sha256"`
	Target   string            `json:"target"` // app slug, "" = none chosen
	Path     string            `json:"path"`   // the target's import path
	Extra    map[string]string `json:"extra"`  // extra form fields (list, account)
	Include  bool              `json:"include"`
	Manual   string            `json:"manual"` // set when Move can't send it: what to do by hand
	Status   string            `json:"status"` // waiting | sent | failed | skipped | done-before
	Result   json.RawMessage   `json:"result"`
	Error    string            `json:"error"`
	SentAt   string            `json:"sent_at"`
	Sort     int               `json:"sort"`
}

// Settings are the box-wide values plus one person's preferences.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`

	NotifyURL    string `json:"notify_url"`
	NotifyFormat string `json:"notify_format"` // apprise | ntfy | json
	// NotifyDone sends a message when an archive is ready or a move-in finished.
	NotifyDone bool `json:"notify_done"`
	// OpenFormats adds the open-format files (ics, vcf, Markdown, CSV) next to each app's own export.
	OpenFormats bool `json:"open_formats"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
