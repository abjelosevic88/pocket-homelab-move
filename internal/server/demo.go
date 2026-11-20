package server

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-move/internal/store"
)

// ---- a stand-in for the box's apps (DEMO_DATA without Pocket Core) ----

type demoApp struct {
	out, in string // link state: "on" or a refusal reason
	exports []core.PortableExport
	imports []core.PortableImport
}

func pk(path, file, title string) core.PortableExport {
	return core.PortableExport{Path: path, File: file, Kind: core.KindPocket, Title: title}
}

var demoApps = map[string]demoApp{
	"notes": {"on", "on", []core.PortableExport{pk("/api/v1/export.zip", "notes.zip", "Every note as a Markdown file, with its folders")},
		[]core.PortableImport{{Path: "/api/v1/import", Kind: core.KindPocket, Form: "file"}, {Path: "/api/v1/import", Kind: core.KindMarkdownZip, Form: "file", Title: "Adds Markdown files as notes"}}},
	"tasks": {"on", "on", []core.PortableExport{pk("/api/v1/export.json", "tasks.json", "Lists, tasks and areas"), {Path: "/api/v1/export.ics", File: "tasks.ics", Kind: core.KindICalTodos}},
		[]core.PortableImport{{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json"}, {Path: "/api/v1/import/ics", Kind: core.KindICalTodos, Form: "file"}}},
	"contacts": {"on", "on", []core.PortableExport{{Path: "/api/v1/export.vcf", File: "contacts.vcf", Kind: core.KindVCard, Title: "Everyone, as vCards"}},
		[]core.PortableImport{{Path: "/api/v1/import", Kind: core.KindVCard, Form: "file"}}},
	"calendar": {"on", "on", []core.PortableExport{{Path: "/api/v1/export.ics", File: "calendar.ics", Kind: core.KindICalEvents, Title: "Every calendar and event"}},
		[]core.PortableImport{{Path: "/api/v1/import", Kind: core.KindICalEvents, Form: "file"}}},
	"money": {"on", "off_person", []core.PortableExport{pk("/api/v1/export/money.json", "money.json", "Accounts, categories, transactions, budgets"), {Path: "/api/v1/export/transactions.csv", File: "transactions.csv", Kind: core.KindCSV}},
		[]core.PortableImport{{Path: "/api/v1/import/bank.csv", Kind: core.KindBankCSV, Form: "file"}}},
	"places": {"on", "on", []core.PortableExport{pk("/api/v1/export.json", "places.json", "Places, lists and visits")},
		[]core.PortableImport{{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json"}, {Path: "/api/v1/import", Kind: core.KindPlaces, Form: "file"}, {Path: "/api/v1/import/csv", Kind: core.KindCSV, Form: "file"}}},
	"podcasts": {"on", "on", []core.PortableExport{pk("/api/v1/export.json", "podcasts.json", "Subscriptions and where you are in each episode"), {Path: "/api/v1/export.opml", File: "podcasts.opml", Kind: core.KindOPMLPodcast}},
		[]core.PortableImport{{Path: "/api/v1/import/opml", Kind: core.KindOPMLPodcast, Form: "file"}, {Path: "/api/v1/import/youtube.csv", Kind: "youtube-subscriptions-csv", Form: "file"}}},
	"reader": {"on", "on", []core.PortableExport{pk("/api/v1/export.json", "reader.json", "Feeds, saved articles and highlights"), {Path: "/api/v1/export.opml", File: "feeds.opml", Kind: core.KindOPMLFeeds}},
		[]core.PortableImport{{Path: "/api/v1/import/opml", Kind: core.KindOPMLFeeds, Form: "file"}}},
	"bookshelf": {"on", "on", []core.PortableExport{pk("/api/v1/export.json", "bookshelf.json", "Books, reviews, quotes"), {Path: "/api/v1/export.csv", File: "books.csv", Kind: core.KindCSV}},
		[]core.PortableImport{{Path: "/api/v1/import/goodreads", Kind: core.KindGoodreads, Form: "file"}}},
	"day":      {"on", "on", []core.PortableExport{pk("/api/v1/export.json", "day.json", "Every journal day"), {Path: "/api/v1/export.md", File: "journal.md", Kind: core.KindMarkdown}}, nil},
	"movement": {"on", "on", []core.PortableExport{pk("/api/v1/export", "movement.json", "Trips and visits")}, []core.PortableImport{{Path: "/api/v1/imports", Kind: core.KindTimeline, Form: "file"}}},
	"files":    {"on", "off_admin", []core.PortableExport{pk("/api/v1/export.zip", "files.zip", "Your own files and folders")}, nil},
	"vault":    {"not_offered", "not_offered", nil, nil},
	"garage":   {"not_offered", "not_offered", nil, nil},
	"health":   {"off_admin", "off_admin", nil, nil},
}

type demoLinks struct{}

func newDemoLinks() *demoLinks { return &demoLinks{} }

func (demoLinks) States(ctx context.Context, email string) (map[string][2]linkState, error) {
	out := map[string][2]linkState{}
	for _, a := range pocketApps {
		d, ok := demoApps[a.Slug]
		if !ok || !movable[a.Slug] {
			d = demoApp{out: "not_offered", in: "not_offered"}
		}
		st := func(v string) linkState {
			if v == "on" {
				return linkState{On: true, State: "on"}
			}
			return linkState{State: v, Reason: linkReason(a.Name, v)}
		}
		out[a.Slug] = [2]linkState{st(d.out), st(d.in)}
	}
	return out, nil
}

func (demoLinks) Describe(ctx context.Context, email, slug, scope string) (*core.Portable, error) {
	d, ok := demoApps[slug]
	if !ok || (scope == core.ScopeMoveExport && d.out != "on") || (scope == core.ScopeMoveImport && d.in != "on") {
		return nil, fmt.Errorf("%s can't do this yet: it needs an update", appName(slug))
	}
	return &core.Portable{Exports: d.exports, Imports: d.imports}, nil
}

func (demoLinks) Get(ctx context.Context, email, slug, path string) (io.ReadCloser, error) {
	body := fmt.Sprintf("{\n  \"app\": \"pocket-%s\",\n  \"exported_for\": %q,\n  \"items\": %d\n}\n", slug, email, len(slug)*37)
	switch {
	case strings.HasSuffix(path, ".ics"):
		body = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Pocket//Demo//EN\r\nEND:VCALENDAR\r\n"
	case strings.HasSuffix(path, ".vcf"):
		body = "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ana Petrović\r\nEMAIL:ana@example.com\r\nEND:VCARD\r\n"
	case strings.HasSuffix(path, ".opml"):
		body = `<?xml version="1.0"?><opml version="2.0"><body><outline text="Example" type="rss" xmlUrl="https://example.com/feed"/></body></opml>`
	case strings.HasSuffix(path, ".zip"):
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		w, _ := zw.Create("Welcome.md")
		_, _ = io.WriteString(w, "# Welcome\n\nA demo note.\n")
		_ = zw.Close()
		body = b.String()
	}
	return io.NopCloser(strings.NewReader(strings.Repeat(body, 1))), nil
}

func (demoLinks) Send(ctx context.Context, email, slug string, imp core.PortableImport, file string, extra map[string]string) (int, []byte, error) {
	st, err := os.Stat(file)
	if err != nil {
		return 0, nil, err
	}
	n := int(st.Size()/700) + 3
	return 200, []byte(fmt.Sprintf(`{"imported": %d, "skipped": %d}`, n, n/9)), nil
}

// ---- demo data: a finished archive, a sorted Google Takeout, an Evernote move ----

func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	ctx = context.WithoutCancel(ctx)
	if list, _ := s.store.ListMoveIn(ctx, owner); len(list) > 0 {
		return nil
	}
	if !s.demoLinks {
		return nil
	}
	// moving out
	m, err := s.store.CreateMoveOut(ctx, owner, []string{"notes", "tasks", "contacts", "calendar", "money", "places", "podcasts", "reader", "bookshelf", "day"})
	if err != nil {
		return err
	}
	if err := s.runOut(ctx, m); err != nil {
		return err
	}
	// an Evernote export, brought in already
	if in, err := s.demoUpload(ctx, owner, "Evernote.enex", demoENEX()); err == nil {
		s.runIn(ctx, in)
	}
	// a Google Takeout, sorted and waiting
	_, err = s.demoUpload(ctx, owner, "takeout-20261118T091544Z-001.zip", demoTakeout())
	return err
}

func (s *Server) demoUpload(ctx context.Context, uid int64, name string, data []byte) (*store.MoveIn, error) {
	m, err := s.store.CreateMoveIn(ctx, uid, name)
	if err != nil {
		return nil, err
	}
	dir := s.inDir(uid, m.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	m.File = filepath.Join(dir, "upload"+filepath.Ext(name))
	if err := os.WriteFile(m.File, data, 0o640); err != nil {
		return nil, err
	}
	m.SizeBytes, m.Status = int64(len(data)), "scanning"
	if err := s.store.SaveMoveIn(ctx, m); err != nil {
		return nil, err
	}
	if err := s.scanIn(ctx, m); err != nil {
		return nil, err
	}
	return s.store.GetMoveIn(ctx, uid, m.ID)
}

func demoENEX() []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE en-export SYSTEM "http://xml.evernote.com/pub/evernote-export4.dtd"><en-export>`)
	notes := []struct{ title, body string }{
		{"Packing list for Lisbon", `<div><en-todo checked="true"/>Passports</div><div><en-todo/>Chargers</div><div><en-todo/>Sun cream</div>`},
		{"Flat viewing notes", `<h2>Vračar, 3rd floor</h2><div>Bright, <b>quiet street</b>, lift works.</div><ul><li>Rent 850 €</li><li>Heating: district</li></ul>`},
		{"Bread recipe", `<div>500 g flour, 350 g water, 10 g salt, 100 g starter.</div><div>Fold four times, 30 minutes apart.</div>`},
	}
	for i := 0; i < 24; i++ {
		n := notes[i%len(notes)]
		title := n.title
		if i >= len(notes) {
			title = fmt.Sprintf("%s (%d)", n.title, i/len(notes)+1)
		}
		fmt.Fprintf(&b, `<note><title>%s</title><content><![CDATA[<?xml version="1.0" encoding="UTF-8" standalone="no"?><!DOCTYPE en-note SYSTEM "http://xml.evernote.com/pub/enml2.dtd"><en-note>%s</en-note>]]></content><created>2024%02d%02dT091500Z</created><updated>2025%02d%02dT181000Z</updated><tag>home</tag></note>`,
			title, n.body, i%12+1, i%27+1, i%12+1, i%27+1)
	}
	b.WriteString(`</en-export>`)
	return []byte(b.String())
}

func demoTakeout() []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	add := func(name, body string) {
		w, _ := zw.Create(name)
		_, _ = io.WriteString(w, body)
	}
	var vc strings.Builder
	names := []string{"Ana Petrović", "Marko Jović", "Lena Horvat", "Ivan Kovač", "Sara Novak", "Petar Ilić", "Mia Babić", "Nikola Marić"}
	for i := 0; i < 212; i++ {
		fmt.Fprintf(&vc, "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:%s %d\r\nEMAIL:person%d@example.com\r\nEND:VCARD\r\n", names[i%len(names)], i, i)
	}
	add("Takeout/Contacts/All Contacts/All Contacts.vcf", vc.String())
	add("Takeout/Contacts/Starred in Android/Starred in Android.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ana Petrović\r\nEND:VCARD\r\n")
	var ics strings.Builder
	ics.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-CALNAME:Family\r\n")
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&ics, "BEGIN:VEVENT\r\nUID:demo-%d@example.com\r\nDTSTART:2026%02d%02dT170000Z\r\nSUMMARY:Football practice\r\nEND:VEVENT\r\n", i, i%12+1, i%27+1)
	}
	ics.WriteString("END:VCALENDAR\r\n")
	add("Takeout/Calendar/Family.ics", ics.String())
	for i, t := range []string{"Gift ideas", "Car service", "Books to read", "Wi-Fi at grandma's"} {
		add(fmt.Sprintf("Takeout/Keep/%s.json", t), fmt.Sprintf(`{"color":"DEFAULT","isTrashed":false,"isPinned":%v,"isArchived":false,"textContent":"Notes about %s","title":%q,"userEditedTimestampUsec":%d,"createdTimestampUsec":%d,"labels":[{"name":"family"}]}`,
			i == 0, strings.ToLower(t), t, 1730000000000000+int64(i)*86400000000, 1720000000000000+int64(i)*86400000000))
	}
	add("Takeout/Tasks/Tasks.json", `{"kind":"tasks#taskLists","items":[{"kind":"tasks#taskList","id":"L1","title":"Groceries","items":[{"kind":"tasks#task","id":"t1","title":"Milk","status":"needsAction"},{"kind":"tasks#task","id":"t2","title":"Eggs","status":"completed","completed":"2026-11-02T10:00:00.000Z"}]},{"kind":"tasks#taskList","id":"L2","title":"House","items":[{"kind":"tasks#task","id":"t3","title":"Bleed the radiators","notes":"Before the first cold week","due":"2026-11-25T00:00:00.000Z","status":"needsAction"}]}]}`)
	add("Takeout/Maps (your places)/Saved Places.json", `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[20.4573,44.8125]},"properties":{"Google Maps URL":"http://maps.google.com/?cid=1","Title":"Kafeterija","Location":{"Address":"Belgrade"}}},{"type":"Feature","geometry":{"type":"Point","coordinates":[-9.1393,38.7223]},"properties":{"Google Maps URL":"http://maps.google.com/?cid=2","Title":"Time Out Market","Location":{"Address":"Lisbon"}}}]}`)
	add("Takeout/YouTube and YouTube Music/subscriptions/subscriptions.csv", "Channel Id,Channel Url,Channel Title\nUC1,http://www.youtube.com/channel/UC1,Veritasium\nUC2,http://www.youtube.com/channel/UC2,Kurzgesagt\nUC3,http://www.youtube.com/channel/UC3,Technology Connections\n")
	add("Takeout/Google Photos/Lisbon 2026/IMG_2041.jpg", "not really a photo")
	add("Takeout/Google Photos/Lisbon 2026/IMG_2042.jpg", "not really a photo")
	add("Takeout/Chrome/History.json", `{"Browser History":[]}`)
	add("Takeout/archive_browser.html", "<html></html>")
	_ = zw.Close()
	return b.Bytes()
}
