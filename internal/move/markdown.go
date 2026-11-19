package move

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// Note is one note on its way into a Markdown zip.
type Note struct {
	Folder  string // slash path, "" = top level
	Title   string
	Body    string // Markdown
	Created time.Time
	Updated time.Time
	Tags    []string
	Files   []NoteFile // attachments, written into "<title> files/"
}

// NoteFile is an attachment of a note.
type NoteFile struct {
	Name string
	Data []byte
	open func() (io.ReadCloser, error) // instead of Data, for big ones
}

// MarkdownZip writes notes as a zip of .md files with YAML front matter
// (title, created, updated, tags), folders kept, attachments next to them.
type MarkdownZip struct {
	zw    *zip.Writer
	taken map[string]bool
	Count int
}

// NewMarkdownZip starts a Markdown zip on w.
func NewMarkdownZip(w io.Writer) *MarkdownZip {
	return &MarkdownZip{zw: zip.NewWriter(w), taken: map[string]bool{}}
}

// unique returns a path no other file in the zip has yet.
func (m *MarkdownZip) unique(p string) string {
	ext := path.Ext(p)
	base := strings.TrimSuffix(p, ext)
	cand := p
	for i := 2; m.taken[strings.ToLower(cand)]; i++ {
		cand = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	m.taken[strings.ToLower(cand)] = true
	return cand
}

// Add writes one note and its attachments. Links to attachments in the body
// must use the placeholder "{{files}}/" which becomes the attachment folder.
func (m *MarkdownZip) Add(n Note) error {
	folder := cleanName(n.Folder)
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = firstWords(n.Body)
	}
	name := safeFile(title)
	mdPath := m.unique(path.Join(folder, name+".md"))
	filesDir := strings.TrimSuffix(path.Base(mdPath), ".md") + " files"
	body := strings.ReplaceAll(n.Body, "{{files}}/", urlPath(filesDir)+"/")

	var fm strings.Builder
	fm.WriteString("---\n")
	fm.WriteString("title: " + yamlString(title) + "\n")
	if !n.Created.IsZero() {
		fm.WriteString("created: " + n.Created.UTC().Format(time.RFC3339) + "\n")
	}
	if !n.Updated.IsZero() {
		fm.WriteString("updated: " + n.Updated.UTC().Format(time.RFC3339) + "\n")
	}
	if len(n.Tags) > 0 {
		var ts []string
		for _, t := range n.Tags {
			if t = strings.TrimSpace(t); t != "" {
				ts = append(ts, yamlString(t))
			}
		}
		if len(ts) > 0 {
			fm.WriteString("tags: [" + strings.Join(ts, ", ") + "]\n")
		}
	}
	fm.WriteString("---\n\n")
	hdr := &zip.FileHeader{Name: mdPath, Method: zip.Deflate}
	if !n.Updated.IsZero() {
		hdr.Modified = n.Updated
	} else if !n.Created.IsZero() {
		hdr.Modified = n.Created
	}
	w, err := m.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, fm.String()+strings.TrimSpace(body)+"\n"); err != nil {
		return err
	}
	for _, f := range n.Files {
		p := path.Join(path.Dir(mdPath), filesDir, safeFile(f.Name))
		w, err := m.zw.Create(p)
		if err != nil {
			return err
		}
		if f.open != nil {
			rc, err := f.open()
			if err != nil {
				continue
			}
			_, err = io.Copy(w, rc)
			rc.Close()
			if err != nil {
				return err
			}
		} else if _, err := w.Write(f.Data); err != nil {
			return err
		}
	}
	m.Count++
	return nil
}

// AddRaw copies a file as it is (an attachment Notion or a Markdown folder already placed).
func (m *MarkdownZip) AddRaw(p string, r io.Reader) error {
	w, err := m.zw.Create(m.unique(cleanName(p)))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

// Close finishes the zip.
func (m *MarkdownZip) Close() error { return m.zw.Close() }

func yamlString(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// urlPath escapes a path for a Markdown link (spaces and brackets).
func urlPath(p string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29").Replace(p)
}

// firstWords is a title from the start of a note body.
func firstWords(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#-*>[] x"))
		if line != "" {
			r := []rune(line)
			if len(r) > 60 {
				r = r[:60]
			}
			return string(r)
		}
	}
	return "Untitled"
}
