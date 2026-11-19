package move

import (
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Notion's "Markdown & CSV" export names every page and folder
// "Title <32 hex id>" and links pages to each other with those names.
var notionID = regexp.MustCompile(`\s[0-9a-f]{32}(\.[A-Za-z0-9]+)?$`)

// isNotionName says whether a path part carries a Notion id.
func isNotionName(name string) bool { return notionID.MatchString(path.Base(name)) }

// stripNotion removes the ids from every part of a path.
func stripNotion(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if m := notionID.FindStringSubmatchIndex(part); m != nil {
			ext := ""
			if m[2] >= 0 {
				ext = part[m[2]:m[3]]
			}
			parts[i] = strings.TrimSpace(part[:m[0]]) + ext
		}
	}
	return strings.Join(parts, "/")
}

var mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// fixNotionLinks rewrites links between pages and to attachments so they
// point at the renamed files. Web links are left alone.
func fixNotionLinks(body string) string {
	return mdLink.ReplaceAllStringFunc(body, func(m string) string {
		target := m[2 : len(m)-1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
			return m
		}
		dec, err := url.PathUnescape(target)
		if err != nil {
			return m
		}
		return "](" + urlPath(stripNotion(dec)) + ")"
	})
}

// NotionToMarkdown copies a Notion export's pages and files under the folder,
// without the ids. Pages keep their own Markdown; a first "# Title" line is
// used as the title by Pocket Notes when there is no front matter.
func NotionToMarkdown(entries []Entry, root, folder string, mz *MarkdownZip) error {
	for _, e := range entries {
		rel := strings.TrimPrefix(e.Name, root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		dst := path.Join(folder, stripNotion(rel))
		rc, err := e.Open()
		if err != nil {
			return err
		}
		if strings.EqualFold(path.Ext(e.Name), ".md") {
			b, err := io.ReadAll(io.LimitReader(rc, 32<<20))
			rc.Close()
			if err != nil {
				return err
			}
			if err := mz.AddRaw(dst, strings.NewReader(fixNotionLinks(string(b)))); err != nil {
				return err
			}
			mz.Count++
			continue
		}
		err = mz.AddRaw(dst, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
