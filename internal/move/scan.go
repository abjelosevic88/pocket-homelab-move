package move

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Kinds Move sends (Pocket Core's sdk.Kind* plus app-specific ones).
const (
	KindPocket      = "pocket"
	KindVCard       = "vcard"
	KindICalEvents  = "ical-events"
	KindICalTodos   = "ical-todos"
	KindMarkdownZip = "markdown-zip"
	KindOPMLFeeds   = "opml-feeds"
	KindOPMLPodcast = "opml-podcasts"
	KindGoodreads   = "goodreads-csv"
	KindBankCSV     = "bank-csv"
	KindPlaces      = "geojson-places"
	KindTimeline    = "google-timeline"
	KindZip         = "zip"
	KindGPX         = "gpx"
	KindYouTube     = "youtube-subscriptions-csv"
	KindMapsList    = "csv" // Pocket Places takes Google Maps saved lists on its CSV import
)

// Found is one thing in an upload, ready to be converted and sent.
type Found struct {
	Label     string
	Detail    string
	Kind      string
	Kinds     []string // other kinds it may go as (an OPML can be feeds or podcasts)
	Prefer    string   // the app it belongs in, when there is one obvious place
	Count     int
	FileName  string
	Extra     map[string]string
	Manual    string // set when it can't be sent: what to do instead
	Sensitive bool   // passwords: never kept longer than needed
	Write     func(w io.Writer) error
}

// Result is what Scan found.
type Result struct {
	Source     string
	SourceName string
	Found      []Found
	Notes      []string // what was left out and why
}

const (
	maxJSON = 256 << 20 // a JSON file read whole (Keep notes, Tasks, saved places)
)

var photoExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".heic": true, ".heif": true, ".gif": true, ".webp": true, ".mp4": true, ".mov": true, ".m4v": true, ".3gp": true, ".dng": true, ".raw": true}

// Scan looks through everything in the upload.
func Scan(fs *Fileset) (*Result, error) {
	res := &Result{Source: "files", SourceName: "Files"}
	if r, ok := scanPocketMove(fs); ok {
		return r, nil
	}
	used := map[string]bool{}
	use := func(e Entry) { used[e.Name] = true }
	byName := map[string]Entry{}
	for _, e := range fs.Entries {
		byName[e.Name] = e
	}

	takeout, icloud := false, false
	for _, e := range fs.Entries {
		l := strings.ToLower(e.Name)
		if strings.HasPrefix(l, "takeout/") || strings.Contains(l, "/takeout/") {
			takeout = true
		}
		if strings.Contains(l, "icloud") {
			icloud = true
		}
	}

	// ---- contacts ----
	var vcfs []Entry
	for _, e := range fs.Entries {
		if e.Ext() == ".vcf" || e.Ext() == ".vcard" {
			vcfs = append(vcfs, e)
		}
	}
	if len(vcfs) > 0 {
		// Google writes the same people into several label files; "All Contacts" has everyone.
		pick := vcfs
		for _, e := range vcfs {
			if strings.EqualFold(stem(e.Name), "All Contacts") {
				pick = []Entry{e}
			}
		}
		for _, e := range vcfs {
			use(e)
		}
		n := 0
		for _, e := range pick {
			if rc, err := e.Open(); err == nil {
				n += countBytes(rc, []byte("BEGIN:VCARD"))
				rc.Close()
			}
		}
		res.Found = append(res.Found, Found{Label: "Contacts", Detail: plural(n, "contact", "contacts"), Kind: KindVCard, Prefer: "contacts", Count: n, FileName: "contacts.vcf",
			Write: func(w io.Writer) error {
				for _, e := range pick {
					if err := copyEntry(w, e); err != nil {
						return err
					}
					_, _ = io.WriteString(w, "\r\n")
				}
				return nil
			}})
	}

	// ---- calendars (and reminders in .ics) ----
	for _, e := range fs.Entries {
		if e.Ext() != ".ics" && e.Ext() != ".ical" {
			continue
		}
		use(e)
		rc, err := e.Open()
		if err != nil {
			continue
		}
		info := scanICS(rc)
		rc.Close()
		name := info.Name
		if name == "" {
			name = stem(e.Name)
		}
		e := e
		if info.Events > 0 {
			res.Found = append(res.Found, Found{Label: "Calendar: " + name, Detail: plural(info.Events, "event", "events"), Kind: KindICalEvents, Prefer: "calendar", Count: info.Events,
				FileName: safeFile(name) + ".ics", Extra: map[string]string{"calendar": name}, Write: func(w io.Writer) error { return copyEntry(w, e) }})
		}
		if info.Todos > 0 {
			res.Found = append(res.Found, Found{Label: "Reminders: " + name, Detail: plural(info.Todos, "task", "tasks"), Kind: KindICalTodos, Prefer: "tasks", Count: info.Todos,
				FileName: safeFile(name) + "-tasks.ics", Extra: map[string]string{"list": name}, Write: func(w io.Writer) error { return copyEntry(w, e) }})
		}
	}

	// ---- Google Tasks ----
	for _, e := range fs.Entries {
		if e.Ext() != ".json" || !isGoogleTasks(e.Head(4096)) {
			continue
		}
		use(e)
		data, err := e.ReadAll(maxJSON)
		if err != nil {
			res.Notes = append(res.Notes, err.Error())
			continue
		}
		lists, err := GoogleTasksToICS(data)
		if err != nil {
			res.Notes = append(res.Notes, "Google Tasks could not be read: "+err.Error())
			continue
		}
		for _, l := range lists {
			l := l
			res.Found = append(res.Found, Found{Label: "Tasks: " + l.Name, Detail: plural(l.Count, "task", "tasks") + " from Google Tasks", Kind: KindICalTodos, Prefer: "tasks", Count: l.Count,
				FileName: safeFile(l.Name) + ".ics", Extra: map[string]string{"list": l.Name}, Write: func(w io.Writer) error { _, err := w.Write(l.ICS); return err }})
		}
	}

	// ---- Evernote ----
	var enex []Entry
	for _, e := range fs.Entries {
		if e.Ext() == ".enex" {
			enex = append(enex, e)
			use(e)
		}
	}
	if len(enex) > 0 {
		n := 0
		for _, e := range enex {
			if rc, err := e.Open(); err == nil {
				n += CountENEX(rc)
				rc.Close()
			}
		}
		books := plural(len(enex), "notebook", "notebooks")
		res.Found = append(res.Found, Found{Label: "Evernote notes", Detail: plural(n, "note", "notes") + " in " + books, Kind: KindMarkdownZip, Prefer: "notes", Count: n, FileName: "evernote.zip",
			Write: func(w io.Writer) error {
				mz := NewMarkdownZip(w)
				for _, e := range enex {
					rc, err := e.Open()
					if err != nil {
						return err
					}
					err = ENEXToMarkdown(rc, path.Join("Evernote", safeFile(stem(e.Name))), mz)
					rc.Close()
					if err != nil {
						return fmt.Errorf("%s: %w", e.Base(), err)
					}
				}
				return mz.Close()
			}})
		if res.Source == "files" {
			res.Source, res.SourceName = "evernote", "Evernote"
		}
	}

	// ---- Google Keep ----
	var keep []Entry
	for _, e := range fs.Entries {
		if e.Ext() == ".json" && !used[e.Name] && isKeepJSON(e.Head(64*1024)) {
			keep = append(keep, e)
			use(e)
		}
	}
	if len(keep) > 0 {
		dirs := map[string]bool{}
		for _, e := range keep {
			dirs[path.Dir(e.Name)] = true
		}
		// attachments (and the .html twin of each note) sit next to the JSON
		for _, e := range fs.Entries {
			if dirs[path.Dir(e.Name)] && (photoExt[e.Ext()] || e.Ext() == ".html" || e.Ext() == ".m4a" || e.Ext() == ".awb") {
				use(e)
			}
		}
		res.Found = append(res.Found, Found{Label: "Google Keep notes", Detail: plural(len(keep), "note", "notes"), Kind: KindMarkdownZip, Prefer: "notes", Count: len(keep), FileName: "google-keep.zip",
			Write: func(w io.Writer) error {
				mz := NewMarkdownZip(w)
				for _, e := range keep {
					data, err := e.ReadAll(maxJSON)
					if err != nil {
						continue
					}
					n, err := keepToNote(data, path.Dir(e.Name), byName)
					if err != nil || n == nil {
						continue
					}
					if err := mz.Add(*n); err != nil {
						return err
					}
				}
				return mz.Close()
			}})
	}

	// ---- Notion ----
	var notion []Entry
	for _, e := range fs.Entries {
		if !used[e.Name] && strings.EqualFold(e.Ext(), ".md") && isNotionName(e.Name) {
			notion = append(notion, e)
		}
	}
	if len(notion) > 0 {
		root := commonDir(notion)
		var all []Entry
		for _, e := range fs.Entries {
			if !used[e.Name] && (root == "" || strings.HasPrefix(e.Name, root+"/")) {
				all = append(all, e)
				use(e)
			}
		}
		res.Found = append(res.Found, Found{Label: "Notion pages", Detail: plural(len(notion), "page", "pages"), Kind: KindMarkdownZip, Prefer: "notes", Count: len(notion), FileName: "notion.zip",
			Write: func(w io.Writer) error {
				mz := NewMarkdownZip(w)
				if err := NotionToMarkdown(all, root, "Notion", mz); err != nil {
					return err
				}
				return mz.Close()
			}})
		res.Source, res.SourceName = "notion", "Notion"
	}

	// ---- saved places, timeline ----
	var records, semantic, timeline []Entry
	for _, e := range fs.Entries {
		if used[e.Name] || e.Ext() != ".json" {
			continue
		}
		head := e.Head(8192)
		low := strings.ToLower(e.Name)
		switch {
		case bytes.Contains(head, []byte(`"FeatureCollection"`)) && (bytes.Contains(head, []byte("Google Maps URL")) || bytes.Contains(head, []byte("google_maps_url")) || strings.Contains(low, "saved places")):
			use(e)
			data, err := e.ReadAll(maxJSON)
			n := 0
			if err == nil {
				n = bytes.Count(data, []byte(`"Feature"`))
			}
			e := e
			res.Found = append(res.Found, Found{Label: "Google Maps saved places", Detail: plural(n, "place", "places"), Kind: KindPlaces, Prefer: "places", Count: n, FileName: "saved-places.geojson",
				Extra: map[string]string{"list": "Google Maps"}, Write: func(w io.Writer) error { return copyEntry(w, e) }})
		case strings.HasSuffix(low, "records.json") && bytes.Contains(head, []byte(`"locations"`)):
			records = append(records, e)
			use(e)
		case bytes.Contains(head, []byte(`"semanticSegments"`)) || bytes.Contains(head, []byte(`"rawSignals"`)):
			timeline = append(timeline, e)
			use(e)
		case within(e.Name, "Semantic Location History") || bytes.Contains(head, []byte(`"timelineObjects"`)):
			semantic = append(semantic, e)
			use(e)
		}
	}
	for _, e := range append(records, timeline...) {
		e := e
		res.Found = append(res.Found, Found{Label: "Location history", Detail: fmt.Sprintf("%s (%d MB)", e.Base(), e.Size>>20), Kind: KindTimeline, Prefer: "movement", FileName: e.Base(),
			Write: func(w io.Writer) error { return copyEntry(w, e) }})
	}
	if len(semantic) > 0 {
		if len(records) > 0 {
			res.Notes = append(res.Notes, fmt.Sprintf("%s of month-by-month location history left out: Records.json above has every point already.", plural(len(semantic), "file", "files")))
		} else {
			for _, e := range semantic {
				e := e
				res.Found = append(res.Found, Found{Label: "Location history: " + stem(e.Name), Detail: "Google Timeline, one month", Kind: KindTimeline, Prefer: "movement", FileName: e.Base(),
					Write: func(w io.Writer) error { return copyEntry(w, e) }})
			}
		}
	}

	// ---- feeds and podcasts ----
	for _, e := range fs.Entries {
		if used[e.Name] || (e.Ext() != ".opml" && e.Ext() != ".xml") {
			continue
		}
		head := e.Head(16 * 1024)
		if !bytes.Contains(bytes.ToLower(head), []byte("<opml")) {
			continue
		}
		use(e)
		rc, _ := e.Open()
		n := 0
		if rc != nil {
			n = countBytes(rc, []byte("xmlUrl="))
			rc.Close()
		}
		low := bytes.ToLower(head)
		kind, prefer, label := KindOPMLFeeds, "reader", "Feeds"
		if bytes.Contains(low, []byte("podcast")) || bytes.Contains(low, []byte("overcast")) || bytes.Contains(low, []byte("pocket casts")) || bytes.Contains(low, []byte("castro")) || bytes.Contains(low, []byte("antennapod")) {
			kind, prefer, label = KindOPMLPodcast, "podcasts", "Podcasts"
		}
		e := e
		res.Found = append(res.Found, Found{Label: label + ": " + stem(e.Name), Detail: plural(n, "feed", "feeds"), Kind: kind, Kinds: []string{KindOPMLFeeds, KindOPMLPodcast}, Prefer: prefer, Count: n,
			FileName: safeFile(stem(e.Name)) + ".opml", Write: func(w io.Writer) error { return copyEntry(w, e) }})
	}

	// ---- CSV files ----
	for _, e := range fs.Entries {
		if used[e.Name] || (e.Ext() != ".csv" && e.Ext() != ".tsv") {
			continue
		}
		head := e.Head(16 * 1024)
		kind := sniffCSV(head)
		if kind == csvUnknown {
			continue
		}
		use(e)
		rc, _ := e.Open()
		n := 0
		if rc != nil {
			n = countRows(rc)
			rc.Close()
		}
		e := e
		write := func(w io.Writer) error { return copyEntry(w, e) }
		switch kind {
		case csvGoodreads:
			res.Found = append(res.Found, Found{Label: "Goodreads library", Detail: plural(n, "book", "books"), Kind: KindGoodreads, Prefer: "bookshelf", Count: n, FileName: "goodreads.csv", Write: write})
		case csvStoryGraph:
			res.Found = append(res.Found, Found{Label: "StoryGraph library", Detail: plural(n, "book", "books"), Kind: KindGoodreads, Prefer: "bookshelf", Count: n, FileName: "storygraph.csv", Write: write})
		case csvYouTube:
			res.Found = append(res.Found, Found{Label: "YouTube subscriptions", Detail: plural(n, "channel", "channels") + ", followed as podcasts", Kind: KindYouTube, Prefer: "podcasts", Count: n, FileName: "subscriptions.csv", Write: write})
		case csvMapsList:
			list := stem(e.Name)
			res.Found = append(res.Found, Found{Label: "Google Maps list: " + list, Detail: plural(n, "place", "places"), Kind: KindMapsList, Prefer: "places", Count: n, FileName: safeFile(list) + ".csv",
				Extra: map[string]string{"list": list}, Write: write})
		case csvPasswords:
			res.Found = append(res.Found, Found{Label: "Passwords: " + stem(e.Name), Detail: plural(n, "login", "logins") + " in plain text", Count: n, FileName: e.Base(), Sensitive: true,
				Manual: "Passwords are encrypted on your own devices, so no server can bring them in for you. Open Pocket Vault in the browser, go to Tools → Import data, pick the app this file came from and choose the file there. Then delete this move here: Move removes its copy at once."})
		case csvBank:
			acct := stem(e.Name)
			res.Found = append(res.Found, Found{Label: "Bank statement: " + acct, Detail: plural(n, "line", "lines"), Kind: KindBankCSV, Prefer: "money", Count: n, FileName: safeFile(acct) + ".csv",
				Extra: map[string]string{"account": acct}, Write: write})
		}
	}

	// ---- Markdown or text notes that are not Notion's (Obsidian, Bear, iCloud Notes) ----
	var md []Entry
	for _, e := range fs.Entries {
		if used[e.Name] {
			continue
		}
		if e.Ext() == ".md" || e.Ext() == ".markdown" || (e.Ext() == ".txt" && (within(e.Name, "iCloud Notes") || within(e.Name, "Notes"))) {
			md = append(md, e)
		}
	}
	if len(md) > 0 {
		root := commonDir(md)
		var all []Entry
		for _, e := range fs.Entries {
			if !used[e.Name] && (root == "" || strings.HasPrefix(e.Name, root+"/")) && (e.Ext() == ".md" || e.Ext() == ".markdown" || e.Ext() == ".txt" || photoExt[e.Ext()] || e.Ext() == ".pdf") {
				all = append(all, e)
				use(e)
			}
		}
		label := "Markdown notes"
		if icloud {
			label = "iCloud Notes"
		}
		res.Found = append(res.Found, Found{Label: label, Detail: plural(len(md), "note", "notes"), Kind: KindMarkdownZip, Prefer: "notes", Count: len(md), FileName: "notes.zip",
			Write: func(w io.Writer) error {
				mz := NewMarkdownZip(w)
				folder := "Moved in"
				if root != "" {
					folder = path.Base(root)
				}
				for _, e := range all {
					rel := strings.TrimPrefix(strings.TrimPrefix(e.Name, root), "/")
					if e.Ext() == ".txt" {
						rel = strings.TrimSuffix(rel, ".txt") + ".md"
					}
					rc, err := e.Open()
					if err != nil {
						return err
					}
					err = mz.AddRaw(path.Join(folder, rel), rc)
					rc.Close()
					if err != nil {
						return err
					}
				}
				return mz.Close()
			}})
	}

	// ---- Google Drive ----
	var drive []Entry
	for _, e := range fs.Entries {
		if !used[e.Name] && takeout && within(e.Name, "Drive") {
			drive = append(drive, e)
			use(e)
		}
	}
	if len(drive) > 0 {
		var size int64
		for _, e := range drive {
			size += e.Size
		}
		res.Found = append(res.Found, Found{Label: "Google Drive files", Detail: fmt.Sprintf("%s, %s", plural(len(drive), "file", "files"), humanBytes(size)), Kind: KindZip, Kinds: []string{KindPocket}, Prefer: "files", Count: len(drive), FileName: "google-drive.zip",
			Write: func(w io.Writer) error {
				zw := zip.NewWriter(w)
				for _, e := range drive {
					rel := e.Name[strings.Index(strings.ToLower("/"+e.Name), "/drive/"):]
					rel = strings.TrimPrefix(rel, "Drive/")
					fw, err := zw.CreateHeader(&zip.FileHeader{Name: path.Join("Google Drive", rel), Method: zip.Store})
					if err != nil {
						return err
					}
					if err := copyEntry(fw, e); err != nil {
						return err
					}
				}
				return zw.Close()
			}})
	}

	// ---- photos and videos ----
	nPhotos := 0
	for _, e := range fs.Entries {
		if !used[e.Name] && photoExt[e.Ext()] {
			nPhotos++
			use(e)
		}
	}
	if nPhotos > 0 {
		res.Found = append(res.Found, Found{Label: "Photos and videos", Detail: plural(nPhotos, "file", "files"), Count: nPhotos,
			Manual: "Photos and videos belong in your photo library (Immich, which Pocket Photos shows). Upload them with Immich's own tools, which keep the dates and albums: immich-go reads a Google Takeout zip as it is, or use the Immich app on your phone."})
	}

	// ---- GPX ----
	for _, e := range fs.Entries {
		if !used[e.Name] && e.Ext() == ".gpx" {
			use(e)
			e := e
			res.Found = append(res.Found, Found{Label: "Track: " + stem(e.Name), Detail: "GPS track", Kind: KindGPX, Prefer: "trails", Count: 1, FileName: e.Base(), Write: func(w io.Writer) error { return copyEntry(w, e) }})
		}
	}

	// ---- what was left ----
	left := map[string]int{}
	for _, e := range fs.Entries {
		if used[e.Name] || e.Base() == "archive_browser.html" {
			continue
		}
		parts := strings.Split(e.Name, "/")
		key := parts[0]
		if strings.EqualFold(key, "Takeout") && len(parts) > 2 {
			key = parts[1]
		} else if len(parts) > 2 {
			key = parts[0] + "/" + parts[1]
		} else if len(parts) == 2 {
			key = parts[0]
		}
		left[key]++
	}
	keys := make([]string, 0, len(left))
	for k := range left {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		res.Notes = append(res.Notes, fmt.Sprintf("Not used: %s in %s (no Pocket app takes it yet).", plural(left[k], "file", "files"), k))
	}

	switch {
	case takeout:
		res.Source, res.SourceName = "google-takeout", "Google Takeout"
	case icloud && res.Source == "files":
		res.Source, res.SourceName = "apple", "iCloud"
	}
	return res, nil
}

// commonDir is the deepest folder every entry sits in ("" = the top).
func commonDir(es []Entry) string {
	if len(es) == 0 {
		return ""
	}
	dir := path.Dir(es[0].Name)
	for _, e := range es[1:] {
		for dir != "." && !strings.HasPrefix(e.Name, dir+"/") {
			dir = path.Dir(dir)
		}
	}
	if dir == "." {
		return ""
	}
	return dir
}

func copyEntry(w io.Writer, e Entry) error {
	rc, err := e.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(w, rc)
	return err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return groupDigits(n) + " " + many
}

func groupDigits(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// ---- a Pocket Move archive (moving to another box) ----

// ArchiveFormat marks a Pocket Move archive's manifest.json.
const ArchiveFormat = "pocket-move/1"

// Manifest is manifest.json at the top of a moving-out archive.
type Manifest struct {
	Format  string        `json:"format"`
	Made    string        `json:"made"`
	Person  string        `json:"person"`
	Box     string        `json:"box"`
	Version string        `json:"move_version"`
	Apps    []ManifestApp `json:"apps"`
}

// ManifestApp is one app in the archive.
type ManifestApp struct {
	App   string         `json:"app"`
	Name  string         `json:"name"`
	Files []ManifestFile `json:"files"`
	Error string         `json:"error,omitempty"`
}

// ManifestFile is one file of an app in the archive.
type ManifestFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Title  string `json:"title,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func scanPocketMove(fs *Fileset) (*Result, bool) {
	var man Entry
	found := false
	for _, e := range fs.Entries {
		if e.Base() == "manifest.json" && strings.Count(e.Name, "/") <= 1 && bytes.Contains(e.Head(512), []byte(ArchiveFormat)) {
			man, found = e, true
			break
		}
	}
	if !found {
		return nil, false
	}
	data, err := man.ReadAll(16 << 20)
	if err != nil {
		return nil, false
	}
	var m Manifest
	if json.Unmarshal(data, &m) != nil || m.Format != ArchiveFormat {
		return nil, false
	}
	prefix := strings.TrimSuffix(man.Name, "manifest.json")
	byName := map[string]Entry{}
	for _, e := range fs.Entries {
		byName[e.Name] = e
	}
	res := &Result{Source: "pocket-move", SourceName: "Pocket Move archive"}
	if m.Person != "" {
		res.SourceName = "Pocket Move archive of " + m.Person
	}
	for _, a := range m.Apps {
		var pick *ManifestFile
		for i, f := range a.Files {
			if f.Kind == KindPocket {
				pick = &a.Files[i]
				break
			}
		}
		if pick == nil {
			if len(a.Files) > 0 {
				res.Notes = append(res.Notes, a.Name+": the archive has only open-format files, nothing to bring back in.")
			}
			continue
		}
		e, ok := byName[prefix+pick.Path]
		if !ok {
			res.Notes = append(res.Notes, a.Name+": "+pick.Path+" is missing from the archive.")
			continue
		}
		res.Found = append(res.Found, Found{Label: a.Name, Detail: pick.Title, Kind: KindPocket, Prefer: a.App, FileName: path.Base(pick.Path),
			Write: func(w io.Writer) error { return copyEntry(w, e) }})
	}
	return res, true
}
