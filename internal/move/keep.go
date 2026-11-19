package move

import (
	"encoding/json"
	"path"
	"strings"
	"time"
)

// Google Keep (Takeout/Keep/*.json): one JSON file per note, attachments next to it.
type keepNote struct {
	Title       string `json:"title"`
	TextContent string `json:"textContent"`
	ListContent []struct {
		Text      string `json:"text"`
		IsChecked bool   `json:"isChecked"`
	} `json:"listContent"`
	IsTrashed  bool `json:"isTrashed"`
	IsArchived bool `json:"isArchived"`
	IsPinned   bool `json:"isPinned"`
	Labels     []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Attachments []struct {
		FilePath string `json:"filePath"`
		Mimetype string `json:"mimetype"`
	} `json:"attachments"`
	Annotations []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"annotations"`
	Edited  int64 `json:"userEditedTimestampUsec"`
	Created int64 `json:"createdTimestampUsec"`
}

// isKeepJSON says whether the JSON looks like a Keep note.
func isKeepJSON(head []byte) bool {
	s := string(head)
	return strings.Contains(s, `"userEditedTimestampUsec"`) && (strings.Contains(s, `"textContent"`) || strings.Contains(s, `"listContent"`) || strings.Contains(s, `"isTrashed"`))
}

func usec(u int64) time.Time {
	if u <= 0 {
		return time.Time{}
	}
	return time.UnixMicro(u).UTC()
}

// keepToNote converts one Keep note; trashed notes come back nil.
// files finds an attachment by its file name in the same folder.
func keepToNote(data []byte, dir string, files map[string]Entry) (*Note, error) {
	var k keepNote
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, err
	}
	if k.IsTrashed {
		return nil, nil
	}
	n := &Note{Folder: "Google Keep", Title: k.Title, Created: usec(k.Created), Updated: usec(k.Edited)}
	if k.IsArchived {
		n.Folder = "Google Keep/Archive"
	}
	for _, l := range k.Labels {
		n.Tags = append(n.Tags, l.Name)
	}
	if k.IsPinned {
		n.Tags = append(n.Tags, "pinned")
	}
	var b strings.Builder
	if k.TextContent != "" {
		b.WriteString(k.TextContent)
	}
	if len(k.ListContent) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		for _, it := range k.ListContent {
			box := "[ ]"
			if it.IsChecked {
				box = "[x]"
			}
			b.WriteString("- " + box + " " + strings.ReplaceAll(it.Text, "\n", " ") + "\n")
		}
	}
	for _, a := range k.Annotations {
		if a.URL != "" {
			t := a.Title
			if t == "" {
				t = a.URL
			}
			b.WriteString("\n[" + t + "](" + a.URL + ")\n")
		}
	}
	for _, a := range k.Attachments {
		name := path.Base(a.FilePath)
		e, ok := files[path.Join(dir, name)]
		if !ok {
			// Keep sometimes names the file .jpeg while it is stored as .jpg
			alt := strings.TrimSuffix(name, path.Ext(name))
			for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".3gp", ".m4a"} {
				if e2, ok2 := files[path.Join(dir, alt+ext)]; ok2 {
					e, ok, name = e2, true, alt+ext
					break
				}
			}
		}
		if !ok {
			continue
		}
		n.Files = append(n.Files, NoteFile{Name: name, open: e.Open})
		link := "{{files}}/" + urlPath(safeFile(name))
		if strings.HasPrefix(a.Mimetype, "image/") {
			b.WriteString("\n![" + name + "](" + link + ")\n")
		} else {
			b.WriteString("\n[" + name + "](" + link + ")\n")
		}
	}
	n.Body = b.String()
	return n, nil
}
