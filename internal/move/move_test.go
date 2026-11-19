package move

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = io.WriteString(w, body)
	}
	_ = zw.Close()
	_ = f.Close()
	return p
}

func scanFiles(t *testing.T, files map[string]string) *Result {
	t.Helper()
	fs, err := Open(writeZip(t, files), "in.zip", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fs.Close)
	res, err := Scan(fs)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func found(res *Result, label string) *Found {
	for i := range res.Found {
		if strings.HasPrefix(res.Found[i].Label, label) {
			return &res.Found[i]
		}
	}
	return nil
}

func render(t *testing.T, f *Found) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestTakeout(t *testing.T) {
	res := scanFiles(t, map[string]string{
		"Takeout/Contacts/All Contacts/All Contacts.vcf":                    "BEGIN:VCARD\nFN:A\nEND:VCARD\nBEGIN:VCARD\nFN:B\nEND:VCARD\n",
		"Takeout/Contacts/Starred/Starred.vcf":                              "BEGIN:VCARD\nFN:A\nEND:VCARD\n",
		"Takeout/Calendar/Work.ics":                                         "BEGIN:VCALENDAR\nX-WR-CALNAME:Work\nBEGIN:VEVENT\nUID:1\nEND:VEVENT\nEND:VCALENDAR\n",
		"Takeout/Keep/Shopping.json":                                        `{"title":"Shopping","isTrashed":false,"listContent":[{"text":"Milk","isChecked":true},{"text":"Eggs","isChecked":false}],"userEditedTimestampUsec":1700000000000000,"labels":[{"name":"home"}]}`,
		"Takeout/Keep/Old.json":                                             `{"title":"Old","isTrashed":true,"textContent":"gone","userEditedTimestampUsec":1}`,
		"Takeout/Tasks/Tasks.json":                                          `{"kind":"tasks#taskLists","items":[{"id":"L","title":"House","items":[{"id":"a","title":"Paint","notes":"blue","status":"needsAction","due":"2026-12-01T00:00:00.000Z"},{"id":"b","title":"Buy brush","status":"completed","parent":"a"}]}]}`,
		"Takeout/Maps (your places)/Saved Places.json":                      `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"Google Maps URL":"u","Title":"Cafe"}}]}`,
		"Takeout/YouTube and YouTube Music/subscriptions/subscriptions.csv": "Channel Id,Channel Url,Channel Title\nUC1,u,One\n",
		"Takeout/Location History (Timeline)/Records.json":                  `{"locations":[{"latitudeE7":1,"longitudeE7":2}]}`,
		"Takeout/Google Photos/Trip/a.jpg":                                  "x",
		"Takeout/Drive/Docs/plan.txt":                                       "the plan",
		"Takeout/Chrome/History.json":                                       `{}`,
	})
	if res.Source != "google-takeout" {
		t.Fatalf("source %q", res.Source)
	}
	c := found(res, "Contacts")
	if c == nil || c.Count != 2 || c.Kind != KindVCard {
		t.Fatalf("contacts %+v", c)
	}
	if found(res, "Calendar: Work") == nil {
		t.Fatal("no calendar")
	}
	k := found(res, "Google Keep")
	if k == nil || k.Count != 2 {
		t.Fatalf("keep %+v", k)
	}
	notes := unzip(t, render(t, k))
	var shop string
	for name, body := range notes {
		if strings.Contains(name, "Shopping") {
			shop = body
		}
		if strings.Contains(name, "Old") {
			t.Error("trashed Keep note was converted")
		}
	}
	if !strings.Contains(shop, "- [x] Milk") || !strings.Contains(shop, "- [ ] Eggs") || !strings.Contains(shop, `tags: ["home"]`) {
		t.Errorf("keep note:\n%s", shop)
	}
	tk := found(res, "Tasks: House")
	if tk == nil || tk.Count != 2 || tk.Kind != KindICalTodos {
		t.Fatalf("tasks %+v", tk)
	}
	ics := string(render(t, tk))
	if !strings.Contains(ics, "X-WR-CALNAME:House") || !strings.Contains(ics, "DUE;VALUE=DATE:20261201") || !strings.Contains(ics, "STATUS:COMPLETED") || !strings.Contains(ics, "RELATED-TO;RELTYPE=PARENT:") {
		t.Errorf("tasks ics:\n%s", ics)
	}
	for _, l := range []string{"Google Maps saved places", "YouTube subscriptions", "Location history", "Google Drive files", "Photos and videos"} {
		if found(res, l) == nil {
			t.Errorf("missing %s", l)
		}
	}
	if p := found(res, "Photos and videos"); p.Manual == "" || p.Write != nil {
		t.Error("photos must be by hand")
	}
	drive := unzip(t, render(t, found(res, "Google Drive files")))
	if drive["Google Drive/Docs/plan.txt"] != "the plan" {
		t.Errorf("drive zip %v", drive)
	}
	left := strings.Join(res.Notes, "\n")
	if !strings.Contains(left, "Chrome") {
		t.Errorf("notes %v", res.Notes)
	}
}

func TestEvernote(t *testing.T) {
	enex := `<?xml version="1.0"?><en-export><note><title>Trip</title><content><![CDATA[<en-note><h2>Pack</h2><div><en-todo checked="true"/>Passport</div><div><en-todo/>Socks</div><div>See <a href="https://x.org">this</a> and <b>bold</b></div><en-media hash="5d41402abc4b2a76b9719d911017c592" type="image/png"/></en-note>]]></content><created>20240102T030405Z</created><tag>travel</tag><resource><data encoding="base64">aGVsbG8=</data><mime>image/png</mime><resource-attributes><file-name>pic.png</file-name></resource-attributes></resource></note></en-export>`
	res := scanFiles(t, map[string]string{"Holidays.enex": enex})
	f := found(res, "Evernote")
	if f == nil || f.Count != 1 || res.Source != "evernote" {
		t.Fatalf("%+v %s", f, res.Source)
	}
	files := unzip(t, render(t, f))
	md := files["Evernote/Holidays/Trip.md"]
	for _, want := range []string{"## Pack", "[x] Passport", "[ ] Socks", "[this](https://x.org)", "**bold**", "![pic.png](Trip%20files/pic.png)", `created: 2024-01-02T03:04:05Z`, `tags: ["travel"]`} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
	if files["Evernote/Holidays/Trip files/pic.png"] != "hello" {
		t.Errorf("attachment: %v", files)
	}
}

func TestNotion(t *testing.T) {
	res := scanFiles(t, map[string]string{
		"Export/Home 0123456789abcdef0123456789abcdef.md":                                          "# Home\n\nSee [Recipes](Home%200123456789abcdef0123456789abcdef/Recipes%20fedcba9876543210fedcba9876543210.md) and [web](https://n.so/a)",
		"Export/Home 0123456789abcdef0123456789abcdef/Recipes fedcba9876543210fedcba9876543210.md": "# Recipes",
		"Export/Home 0123456789abcdef0123456789abcdef/photo.png":                                   "img",
	})
	f := found(res, "Notion")
	if f == nil || f.Count != 2 || res.Source != "notion" {
		t.Fatalf("%+v", res)
	}
	files := unzip(t, render(t, f))
	if !strings.Contains(files["Notion/Home.md"], "[Recipes](Home/Recipes.md)") || !strings.Contains(files["Notion/Home.md"], "(https://n.so/a)") {
		t.Errorf("links: %v", files)
	}
	if _, ok := files["Notion/Home/Recipes.md"]; !ok {
		t.Errorf("files: %v", files)
	}
	if files["Notion/Home/photo.png"] != "img" {
		t.Errorf("attachment: %v", files)
	}
}

func TestSingleFilesAndCSV(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, body, label, kind string
		manual                  bool
	}{
		{"goodreads_library_export.csv", "Book Id,Title,Author,ISBN\n1,A,B,\n2,C,D,\n", "Goodreads", KindGoodreads, false},
		{"Checking.csv", "Date;Description;Amount\n2026-01-01;Rent;-800\n", "Bank statement: Checking", KindBankCSV, false},
		{"1Password.csv", "Title,Url,Username,Password\nx,y,z,secret\n", "Passwords", "", true},
		{"feeds.opml", `<?xml version="1.0"?><opml><head><title>Feedly</title></head><body><outline xmlUrl="a"/><outline xmlUrl="b"/></body></opml>`, "Feeds", KindOPMLFeeds, false},
		{"Overcast.opml", `<opml><head><title>Overcast Podcast Subscriptions</title></head><body><outline type="rss" xmlUrl="a"/></body></opml>`, "Podcasts", KindOPMLPodcast, false},
		{"me.vcf", "BEGIN:VCARD\nFN:Me\nEND:VCARD\n", "Contacts", KindVCard, false},
		{"rem.ics", "BEGIN:VCALENDAR\nBEGIN:VTODO\nSUMMARY:x\nEND:VTODO\nEND:VCALENDAR\n", "Reminders", KindICalTodos, false},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		_ = os.WriteFile(p, []byte(c.body), 0o600)
		fs, err := Open(p, c.name, dir)
		if err != nil {
			t.Fatal(err)
		}
		res, _ := Scan(fs)
		fs.Close()
		f := found(res, c.label)
		if f == nil {
			t.Errorf("%s: nothing found (%v)", c.name, res.Found)
			continue
		}
		if c.manual {
			if f.Manual == "" || f.Write != nil || !f.Sensitive {
				t.Errorf("%s must be by hand and never written", c.name)
			}
			continue
		}
		if f.Kind != c.kind {
			t.Errorf("%s: kind %s want %s", c.name, f.Kind, c.kind)
		}
	}
}

func TestPocketMoveArchive(t *testing.T) {
	man := `{"format":"pocket-move/1","person":"a@b.c","apps":[{"app":"notes","name":"Pocket Notes","files":[{"path":"notes/notes.zip","kind":"pocket"},{"path":"notes/x.md","kind":"markdown"}]},{"app":"calendar","name":"Pocket Calendar","files":[{"path":"calendar/calendar.ics","kind":"ical-events"}]}]}`
	res := scanFiles(t, map[string]string{
		"pocket-move-2026-11-19-1/manifest.json":         man,
		"pocket-move-2026-11-19-1/notes/notes.zip":       "zipbytes",
		"pocket-move-2026-11-19-1/calendar/calendar.ics": "BEGIN:VCALENDAR\nEND:VCALENDAR",
	})
	if res.Source != "pocket-move" || len(res.Found) != 2 || res.Found[0].Prefer != "notes" || res.Found[0].Kind != KindPocket {
		t.Fatalf("%+v", res)
	}
	if string(render(t, &res.Found[0])) != "zipbytes" {
		t.Error("wrong file")
	}
	// Calendar's whole copy is its .ics: it goes back through the iCalendar import
	if res.Found[1].Prefer != "calendar" || res.Found[1].Kind != KindICalEvents {
		t.Errorf("calendar %+v", res.Found[1])
	}
}

func TestZipSlipNamesAreCleaned(t *testing.T) {
	if got := cleanName("../../etc/passwd"); got != "etc/passwd" {
		t.Fatal(got)
	}
	if got := cleanName(`a\..\b/./c`); got != "a/b/c" {
		t.Fatal(got)
	}
}

func TestHTMLToMarkdownLists(t *testing.T) {
	md := HTMLToMarkdown(`<ul><li>one<ul><li>inner</li></ul></li><li>two</li></ul><ol><li>first</li></ol><table><tr><th>a</th><th>b</th></tr><tr><td>1</td><td>2</td></tr></table>`, nil)
	for _, want := range []string{"- one\n  - inner", "- two", "1. first", "| a | b |", "| 1 | 2 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
}

func TestNestedZipsOnlyWhenNothingElse(t *testing.T) {
	var inner bytes.Buffer
	zw := zip.NewWriter(&inner)
	w, _ := zw.Create("Page 0123456789abcdef0123456789abcdef.md")
	_, _ = io.WriteString(w, "# Page")
	_ = zw.Close()
	res := scanFiles(t, map[string]string{"Export-1/Part-1.zip": inner.String()})
	if found(res, "Notion") == nil {
		t.Fatalf("nested Notion export not opened: %+v", res)
	}
	fs, err := Open(writeZip(t, map[string]string{"a/app.zip": inner.String(), "a/readme.txt": "x"}), "x.zip", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if len(fs.Entries) != 2 {
		t.Fatalf("a zip next to other files was opened: %v", len(fs.Entries))
	}
}
