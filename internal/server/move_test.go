package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-move/internal/store"
)

// recLinks is the demo stand-in that also records who sent what where.
type recLinks struct {
	demoLinks
	mu    sync.Mutex
	sends []string // "email app path kind"
	gets  []string // "email app path"
}

func (r *recLinks) Get(ctx context.Context, email, slug, path string) (io.ReadCloser, error) {
	r.mu.Lock()
	r.gets = append(r.gets, email+" "+slug+" "+path)
	r.mu.Unlock()
	return r.demoLinks.Get(ctx, email, slug, path)
}

func (r *recLinks) Send(ctx context.Context, email, slug string, imp core.PortableImport, file string, extra map[string]string) (int, []byte, error) {
	r.mu.Lock()
	r.sends = append(r.sends, email+" "+slug+" "+imp.Path+" "+imp.Kind)
	r.mu.Unlock()
	for k := range extra {
		if strings.HasPrefix(k, "_") {
			return 500, nil, fmt.Errorf("internal field %s sent to the app", k)
		}
	}
	return r.demoLinks.Send(ctx, email, slug, imp, file, extra)
}

type tenv struct {
	s     *Server
	srv   *httptest.Server
	links *recLinks
	alice string
	bob   string
}

func newEnv(t *testing.T) *tenv {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.MaxUploadBytes, cfg.KeepDays, cfg.MaxBodyBytes = 1<<30, 7, 1<<20
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	links := &recLinks{}
	s.links, s.demoLinks = links, true
	ctx := context.Background()
	mk := func(email, role string) string {
		u := &store.User{Email: email, Name: strings.Split(email, "@")[0], Role: role, PasswordHash: "x"}
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, err := st.CreateAPIToken(ctx, u.ID, "t")
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	e := &tenv{s: s, links: links, alice: mk("alice@example.com", "admin"), bob: mk("bob@example.com", "member")}
	e.srv = httptest.NewServer(s.Router())
	t.Cleanup(e.srv.Close)
	return e
}

func (e *tenv) do(t *testing.T, tok, method, path string, body io.Reader, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil && method != http.MethodPost || strings.HasPrefix(path, "/api/v1/out") {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		b, _ := io.ReadAll(res.Body)
		_ = json.Unmarshal(b, out)
	}
	return res.StatusCode
}

func (e *tenv) waitIn(t *testing.T, tok string, id int64, want ...string) map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var d struct {
			Move  store.MoveIn    `json:"move"`
			Items json.RawMessage `json:"items"`
		}
		e.do(t, tok, "GET", fmt.Sprintf("/api/v1/in/%d", id), nil, &d)
		for _, w := range want {
			if d.Move.Status == w {
				var raw map[string]json.RawMessage
				e.do(t, tok, "GET", fmt.Sprintf("/api/v1/in/%d", id), nil, &raw)
				return raw
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("move %d never became %v", id, want)
	return nil
}

func TestMoveInIsPerPersonAndRunsOnce(t *testing.T) {
	e := newEnv(t)
	var m store.MoveIn
	if code := e.do(t, e.alice, "POST", "/api/v1/in?name=takeout.zip", bytes.NewReader(demoTakeout()), &m); code != 201 {
		t.Fatalf("upload: %d", code)
	}
	raw := e.waitIn(t, e.alice, m.ID, "ready")
	var items []itemView
	_ = json.Unmarshal(raw["items"], &items)
	if len(items) < 6 {
		t.Fatalf("items: %d", len(items))
	}
	var contacts *itemView
	for i := range items {
		if items[i].Label == "Contacts" {
			contacts = &items[i]
		}
		for k := range items[i].Extra {
			if strings.HasPrefix(k, "_") {
				t.Errorf("internal field %s shown on the page", k)
			}
		}
	}
	if contacts == nil || contacts.Target != "contacts" || !contacts.Include || contacts.Count != 212 {
		t.Fatalf("contacts item %+v", contacts)
	}
	// the upload itself is gone once read
	if files, _ := filepath.Glob(filepath.Join(e.s.inDir(1, m.ID), "upload*")); len(files) != 0 {
		t.Errorf("upload kept: %v", files)
	}

	// Bob sees none of it
	var bl []store.MoveIn
	e.do(t, e.bob, "GET", "/api/v1/in", nil, &bl)
	if len(bl) != 0 {
		t.Fatalf("bob sees %d moves", len(bl))
	}
	for _, c := range []struct{ method, path string }{
		{"GET", fmt.Sprintf("/api/v1/in/%d", m.ID)},
		{"POST", fmt.Sprintf("/api/v1/in/%d/run", m.ID)},
		{"DELETE", fmt.Sprintf("/api/v1/in/%d", m.ID)},
		{"PUT", fmt.Sprintf("/api/v1/in/%d/items/%d", m.ID, contacts.ID)},
		{"GET", fmt.Sprintf("/api/v1/in/%d/items/%d/download", m.ID, contacts.ID)},
	} {
		var body io.Reader
		if c.method == "PUT" {
			body = strings.NewReader(`{"include":false}`)
		}
		if code := e.do(t, e.bob, c.method, c.path, body, nil); code != 404 {
			t.Errorf("bob %s %s: %d, want 404", c.method, c.path, code)
		}
	}
	// a target the item can't go to is refused
	if code := e.do(t, e.alice, "PUT", fmt.Sprintf("/api/v1/in/%d/items/%d", m.ID, contacts.ID), strings.NewReader(`{"target":"money /api/v1/import/bank.csv bank-csv"}`), nil); code != 400 {
		t.Errorf("wrong target accepted: %d", code)
	}

	if code := e.do(t, e.alice, "POST", fmt.Sprintf("/api/v1/in/%d/run", m.ID), nil, nil); code != 202 {
		t.Fatalf("run: %d", code)
	}
	raw = e.waitIn(t, e.alice, m.ID, "done")
	_ = json.Unmarshal(raw["items"], &items)
	sent := 0
	for _, it := range items {
		if it.Status == "sent" {
			sent++
		}
		if it.Status == "failed" {
			t.Errorf("%s failed: %s", it.Label, it.Error)
		}
	}
	if sent == 0 {
		t.Fatal("nothing sent")
	}
	for _, s := range e.links.sends {
		if !strings.HasPrefix(s, "alice@example.com ") {
			t.Errorf("sent as someone else: %s", s)
		}
	}

	// the same file again: everything is marked as brought in before, nothing chosen
	var m2 store.MoveIn
	e.do(t, e.alice, "POST", "/api/v1/in?name=takeout.zip", bytes.NewReader(demoTakeout()), &m2)
	raw = e.waitIn(t, e.alice, m2.ID, "ready")
	_ = json.Unmarshal(raw["items"], &items)
	for _, it := range items {
		if it.Target != "" && it.Include {
			t.Errorf("%s chosen again", it.Label)
		}
	}
}

func TestPasswordsAreNeverKept(t *testing.T) {
	e := newEnv(t)
	var m store.MoveIn
	e.do(t, e.alice, "POST", "/api/v1/in?name=bitwarden.csv", strings.NewReader("folder,favorite,type,name,notes,fields,login_uri,login_username,login_password\n,,login,Bank,,,https://bank,me,hunter2\n"), &m)
	raw := e.waitIn(t, e.alice, m.ID, "ready")
	var items []itemView
	_ = json.Unmarshal(raw["items"], &items)
	if len(items) != 1 || items[0].Manual == "" || items[0].Bytes != 0 || items[0].Include {
		t.Fatalf("%+v", items)
	}
	err := filepath.Walk(e.s.inDir(1, m.ID), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			if bytes.Contains(b, []byte("hunter2")) {
				t.Errorf("password kept in %s", p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMoveOutArchive(t *testing.T) {
	e := newEnv(t)
	var m store.MoveOut
	if code := e.do(t, e.alice, "POST", "/api/v1/out", strings.NewReader(`{"apps":["notes","contacts","vault","nosuchapp"]}`), &m); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if len(m.Apps) != 3 {
		t.Fatalf("apps %v", m.Apps)
	}
	deadline := time.Now().Add(10 * time.Second)
	for m.Status != "done" && m.Status != "failed" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		e.do(t, e.alice, "GET", fmt.Sprintf("/api/v1/out/%d", m.ID), nil, &m)
	}
	if m.Status != "done" {
		t.Fatalf("status %s %s", m.Status, m.Error)
	}
	states := map[string]string{}
	for _, r := range m.Results {
		states[r.App] = r.State
	}
	if states["notes"] != "done" || states["contacts"] != "done" || states["vault"] != "failed" {
		t.Errorf("states %v", states)
	}
	if code := e.do(t, e.bob, "GET", fmt.Sprintf("/api/v1/out/%d/download", m.ID), nil, nil); code != 404 {
		t.Errorf("bob download: %d", code)
	}
	if code := e.do(t, e.bob, "DELETE", fmt.Sprintf("/api/v1/out/%d", m.ID), nil, nil); code != 404 {
		t.Errorf("bob delete: %d", code)
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/out/%d/download", e.srv.URL, m.ID), nil)
	req.Header.Set("Authorization", "Bearer "+e.alice)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var man []byte
	for _, f := range zr.File {
		names[f.Name[strings.Index(f.Name, "/")+1:]] = true
		if strings.HasSuffix(f.Name, "manifest.json") {
			rc, _ := f.Open()
			man, _ = io.ReadAll(rc)
			rc.Close()
		}
	}
	for _, want := range []string{"manifest.json", "README.html", "notes/notes.zip", "contacts/contacts.vcf"} {
		if !names[want] {
			t.Errorf("archive lacks %s: %v", want, names)
		}
	}
	if !bytes.Contains(man, []byte(`"person": "alice@example.com"`)) || !bytes.Contains(man, []byte(`"format": "pocket-move/1"`)) {
		t.Errorf("manifest %s", man)
	}
	for _, g := range e.links.gets {
		if !strings.HasPrefix(g, "alice@example.com ") {
			t.Errorf("read as someone else: %s", g)
		}
	}

	// the archive comes back in as a Pocket Move archive, each app into itself
	var in store.MoveIn
	e.do(t, e.alice, "POST", "/api/v1/in?name=archive.zip", bytes.NewReader(data), &in)
	raw := e.waitIn(t, e.alice, in.ID, "ready")
	var mv store.MoveIn
	_ = json.Unmarshal(raw["move"], &mv)
	if mv.Source != "pocket-move" {
		t.Errorf("source %s", mv.Source)
	}
}

func TestLinkTokensReachNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/in", nil)
	if linkAllows([]string{core.ScopeMoveExport, core.ScopeMoveImport}, r) {
		t.Error("a link token may list moves")
	}
	if !linkAllows(nil, httptest.NewRequest("GET", "/api/v1/auth/me", nil)) {
		t.Error("auth/me closed")
	}
}

func TestUsesOnlyInAppsThatCanMove(t *testing.T) {
	uses := linkUses()
	if len(uses) != len(movable) {
		t.Fatalf("%d uses for %d movable apps", len(uses), len(movable))
	}
	for slug := range movable {
		if _, ok := appBySlug(slug); !ok {
			t.Errorf("movable %q is not a known app", slug)
		}
	}
	for _, u := range uses {
		want := 2
		if copyOutOnly[u.App] != "" {
			want = 1
		}
		if u.App == "move" || len(u.Scopes) != want || u.Scopes[0] != core.ScopeMoveExport {
			t.Errorf("use %+v", u)
		}
	}
}
