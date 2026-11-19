// Package move finds what is in a file someone brings in (a Google Takeout
// archive, an Evernote export, a Notion export, an iCloud download, a Pocket
// Move archive or a single file) and converts each part to an open format a
// Pocket app takes (see Pocket Core's docs/move.md for the kinds).
package move

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one file inside what was brought in.
type Entry struct {
	Name string // slash path inside the archive ("Takeout/Contacts/All Contacts.vcf")
	Size int64
	open func() (io.ReadCloser, error)
}

// Open reads the entry.
func (e Entry) Open() (io.ReadCloser, error) { return e.open() }

// Head returns up to n bytes from the start of the entry.
func (e Entry) Head(n int) []byte {
	rc, err := e.open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	buf := make([]byte, n)
	k, _ := io.ReadFull(rc, buf)
	return buf[:k]
}

// ReadAll reads the whole entry, refusing ones larger than max.
func (e Entry) ReadAll(max int64) ([]byte, error) {
	if e.Size > max {
		return nil, fmt.Errorf("%s is too large (%d MB)", e.Name, e.Size>>20)
	}
	rc, err := e.open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, max+1))
}

// Base is the entry's file name.
func (e Entry) Base() string { return path.Base(e.Name) }

// Ext is the lower-case extension with the dot.
func (e Entry) Ext() string { return strings.ToLower(path.Ext(e.Name)) }

// Fileset is everything inside one upload.
type Fileset struct {
	Entries []Entry
	closers []io.Closer
	temps   []string
}

// Close releases open archives and temporary files.
func (fs *Fileset) Close() {
	for _, c := range fs.closers {
		_ = c.Close()
	}
	for _, t := range fs.temps {
		_ = os.Remove(t)
	}
}

// IsZip says whether the file starts like a zip archive.
func IsZip(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := io.ReadFull(f, head)
	return n == 4 && bytes.Equal(head, []byte("PK\x03\x04"))
}

// maxEntries keeps a hostile archive from exhausting memory.
const maxEntries = 500_000

// Open reads an upload: a zip is opened (a zip of nothing but zips, as
// Notion nests its export, is opened one level deeper), any other file is
// one entry named after the upload.
func Open(p, name, tmpDir string) (*Fileset, error) {
	fs := &Fileset{}
	if !IsZip(p) {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		fs.Entries = []Entry{{Name: cleanName(name), Size: st.Size(), open: func() (io.ReadCloser, error) { return os.Open(p) }}}
		return fs, nil
	}
	if err := fs.addZip(p, "", tmpDir, 0); err != nil {
		fs.Close()
		return nil, err
	}
	sort.SliceStable(fs.Entries, func(i, j int) bool { return fs.Entries[i].Name < fs.Entries[j].Name })
	return fs, nil
}

func (fs *Fileset) addZip(p, prefix, tmpDir string, depth int) error {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return fmt.Errorf("the archive can't be read: %w", err)
	}
	fs.closers = append(fs.closers, zr)
	// Notion (and some download tools) wrap the real export in a zip that holds
	// nothing but zips: only then are the inner ones opened. A Pocket Move
	// archive keeps each app's own zip as it is.
	onlyZips := depth == 0
	for _, f := range zr.File {
		n := cleanName(f.Name)
		if f.FileInfo().IsDir() || n == "" || strings.HasPrefix(n, "__MACOSX/") || strings.HasPrefix(path.Base(n), "._") || path.Base(n) == ".DS_Store" {
			continue
		}
		if !strings.EqualFold(path.Ext(n), ".zip") {
			onlyZips = false
			break
		}
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := cleanName(f.Name)
		if name == "" || strings.HasPrefix(path.Base(name), "._") || strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store" {
			continue
		}
		if len(fs.Entries) >= maxEntries {
			return fmt.Errorf("the archive has more than %d files", maxEntries)
		}
		f := f
		if onlyZips && strings.EqualFold(path.Ext(name), ".zip") && tmpDir != "" {
			// a zip inside the zip: unpack it to a temporary file and read it too
			tmp, err := os.CreateTemp(tmpDir, "inner-*.zip")
			if err == nil {
				rc, err2 := f.Open()
				if err2 == nil {
					_, err2 = io.Copy(tmp, rc)
					rc.Close()
				}
				tmp.Close()
				fs.temps = append(fs.temps, tmp.Name())
				if err2 == nil && IsZip(tmp.Name()) {
					inner := strings.TrimSuffix(name, path.Ext(name)) + "/"
					if err := fs.addZip(tmp.Name(), prefix+inner, tmpDir, depth+1); err == nil {
						continue
					}
				}
			}
		}
		fs.Entries = append(fs.Entries, Entry{Name: prefix + name, Size: int64(f.UncompressedSize64), open: f.Open})
	}
	return nil
}

// cleanName makes an archive path safe: forward slashes, no leading slash,
// no "..", no empty parts.
func cleanName(n string) string {
	n = strings.ReplaceAll(n, "\\", "/")
	var parts []string
	for _, p := range strings.Split(n, "/") {
		if p == "" || p == "." || p == ".." {
			continue
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "/")
}

// stem is a file name without its extension.
func stem(name string) string {
	b := path.Base(name)
	return strings.TrimSuffix(b, path.Ext(b))
}

// firstLine is the first line of a byte slice, trimmed, without a BOM.
func firstLine(b []byte) string {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	if sc.Scan() {
		return strings.TrimSpace(sc.Text())
	}
	return ""
}

// safeFile turns a title into a file name that works everywhere.
func safeFile(s string) string {
	s = strings.TrimSpace(s)
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "'", "<", "", ">", "", "|", "-", "\x00", "")
	s = repl.Replace(s)
	s = strings.Trim(s, ". ")
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:120])
	}
	if s == "" {
		s = "Untitled"
	}
	return s
}

// within reports whether the entry sits under a folder (case-insensitive, at any depth).
func within(name, folder string) bool {
	l := "/" + strings.ToLower(name)
	return strings.Contains(l, "/"+strings.ToLower(folder)+"/")
}

// TempPath is where a converted file for one item is written.
func TempPath(dir string, n int, fileName string) string {
	return filepath.Join(dir, fmt.Sprintf("%03d-%s", n, safeFile(fileName)))
}
