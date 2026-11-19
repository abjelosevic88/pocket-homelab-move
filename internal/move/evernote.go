package move

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"io"
	"mime"
	"strings"
	"time"
)

// Evernote .enex: <en-export><note><title/><content>ENML</content><created/>
// <updated/><tag/>…<resource><data encoding="base64"/><mime/>
// <resource-attributes><file-name/></resource-attributes></resource></note>.

type enexNote struct {
	Title     string         `xml:"title"`
	Content   string         `xml:"content"`
	Created   string         `xml:"created"`
	Updated   string         `xml:"updated"`
	Tags      []string       `xml:"tag"`
	Resources []enexResource `xml:"resource"`
}

type enexResource struct {
	Data struct {
		Encoding string `xml:"encoding,attr"`
		Value    string `xml:",chardata"`
	} `xml:"data"`
	Mime  string `xml:"mime"`
	Attrs struct {
		FileName string `xml:"file-name"`
	} `xml:"resource-attributes"`
}

func enexTime(s string) time.Time {
	t, err := time.Parse("20060102T150405Z", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// CountENEX counts the notes in an .enex without converting them.
func CountENEX(r io.Reader) int {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	n := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return n
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "note" {
			n++
			_ = dec.Skip()
		}
	}
}

// ENEXToMarkdown streams the notes of one .enex (one notebook) into the zip,
// in a folder named after the notebook.
func ENEXToMarkdown(r io.Reader, folder string, mz *MarkdownZip) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "note" {
			continue
		}
		var n enexNote
		if err := dec.DecodeElement(&n, &se); err != nil {
			return err
		}
		if err := mz.Add(enexToNote(n, folder)); err != nil {
			return err
		}
	}
}

func enexToNote(n enexNote, folder string) Note {
	note := Note{Folder: folder, Title: strings.TrimSpace(n.Title), Created: enexTime(n.Created), Updated: enexTime(n.Updated), Tags: n.Tags}
	byHash := map[string]string{} // md5 → file name in the attachment folder
	used := map[string]bool{}
	for i, res := range n.Resources {
		data, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, res.Data.Value))
		if err != nil || len(data) == 0 {
			continue
		}
		sum := md5.Sum(data)
		name := safeFile(res.Attrs.FileName)
		if res.Attrs.FileName == "" {
			name = "attachment-" + itoa(i+1)
			if exts, _ := mime.ExtensionsByType(res.Mime); len(exts) > 0 {
				name += exts[0]
			}
		}
		for k := 2; used[strings.ToLower(name)]; k++ {
			name = stem(name) + " " + itoa(k) + extOf(name)
		}
		used[strings.ToLower(name)] = true
		byHash[hex.EncodeToString(sum[:])] = name
		note.Files = append(note.Files, NoteFile{Name: name, Data: data})
	}
	note.Body = HTMLToMarkdown(n.Content, func(a map[string]string) string {
		name, ok := byHash[strings.ToLower(a["hash"])]
		if !ok {
			return ""
		}
		link := "{{files}}/" + urlPath(name)
		if strings.HasPrefix(a["type"], "image/") {
			return "![" + name + "](" + link + ")"
		}
		return "[" + name + "](" + link + ")"
	})
	return note
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[i:]
	}
	return ""
}
