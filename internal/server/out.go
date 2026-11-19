package server

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-move/internal/move"
	"github.com/abjelosevic88/pocket-homelab-move/internal/store"
)

// appView is one app on the Move pages: what Move may do there for this person.
type appView struct {
	pocketApp
	Out     linkState             `json:"out"`
	In      linkState             `json:"in"`
	Exports []core.PortableExport `json:"exports"`
	Imports []core.PortableImport `json:"imports"`
	Error   string                `json:"error,omitempty"`
}

// appViews asks Core for the person's links and every app that is on for what it gives and takes.
func (s *Server) appViews(ctx context.Context, email string) ([]appView, error) {
	states, err := s.links.States(ctx, email)
	if err != nil {
		return nil, err
	}
	views := make([]appView, 0, len(pocketApps))
	for _, a := range pocketApps {
		pair, ok := states[a.Slug]
		if !ok {
			pair = [2]linkState{{State: "not_declared", Reason: linkReason(a.Name, "not_declared")}, {State: "not_declared", Reason: linkReason(a.Name, "not_declared")}}
		}
		views = append(views, appView{pocketApp: a, Out: pair[0], In: pair[1], Exports: []core.PortableExport{}, Imports: []core.PortableImport{}})
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range views {
		v := &views[i]
		if !v.Out.On && !v.In.On {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			scope := core.ScopeMoveExport
			if !v.Out.On {
				scope = core.ScopeMoveImport
			}
			p, err := s.links.Describe(ctx, email, v.Slug, scope)
			if err != nil {
				v.Error = err.Error()
				return
			}
			if v.Out.On {
				v.Exports = append(v.Exports, p.Exports...)
			}
			if v.In.On {
				v.Imports = append(v.Imports, p.Imports...)
			}
		}()
	}
	wg.Wait()
	return views, nil
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if !s.linksReady() {
		writeJSON(w, http.StatusOK, map[string]any{"core": false, "apps": []appView{}})
		return
	}
	views, err := s.appViews(r.Context(), u.Email)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"core": true, "error": "Pocket Core did not answer: " + err.Error(), "apps": []appView{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"core": true, "apps": views})
}

// linksReady says whether Move can reach any app (Core, or the demo stand-in).
func (s *Server) linksReady() bool {
	if s.demoLinks {
		return true
	}
	return s.core.Configured()
}

// ---- moving out ----

func (s *Server) outDir(uid int64) string {
	return filepath.Join(s.cfg.FilesDir, fmt.Sprint(uid), "out")
}

func (s *Server) handleListOut(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMoveOut(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "archives")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetOut(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMoveOut(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "archive")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleCreateOut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Apps []string `json:"apps"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !s.linksReady() {
		writeErr(w, http.StatusConflict, "Moving out needs Pocket Core")
		return
	}
	seen := map[string]bool{}
	var apps []string
	for _, a := range in.Apps {
		if _, ok := appBySlug(a); ok && !seen[a] {
			seen[a] = true
			apps = append(apps, a)
		}
	}
	if len(apps) == 0 {
		writeErr(w, http.StatusBadRequest, "pick at least one app")
		return
	}
	m, err := s.store.CreateMoveOut(r.Context(), actorID(r.Context()), apps)
	if err != nil {
		s.fail(w, err, "archive")
		return
	}
	s.startOut(m)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleDownloadOut(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMoveOut(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "archive")
		return
	}
	if m.Status != "done" || m.File == "" {
		writeErr(w, http.StatusNotFound, "the archive is not ready")
		return
	}
	f, err := os.Open(m.File)
	if err != nil {
		writeErr(w, http.StatusGone, "the archive was removed")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(m.File)+`"`)
	http.ServeContent(w, r, filepath.Base(m.File), st.ModTime(), f)
}

func (s *Server) handleDeleteOut(w http.ResponseWriter, r *http.Request) {
	uid := actorID(r.Context())
	m, err := s.store.GetMoveOut(r.Context(), uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "archive")
		return
	}
	if m.Status == "running" || m.Status == "queued" {
		writeErr(w, http.StatusConflict, "the archive is still being made")
		return
	}
	if m.File != "" {
		_ = os.Remove(m.File)
	}
	if err := s.store.DeleteMoveOut(r.Context(), uid, m.ID); err != nil {
		s.fail(w, err, "archive")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startOut makes the archive in the background (two at a time on the box).
func (s *Server) startOut(m *store.MoveOut) {
	go func() {
		s.workers <- struct{}{}
		defer func() { <-s.workers }()
		ctx := context.Background()
		if err := s.runOut(ctx, m); err != nil {
			m.Status, m.Error, m.FinishedAt = "failed", err.Error(), store.Now()
			_ = s.store.SaveMoveOut(ctx, m)
			s.log.Warn("moving out failed", "id", m.ID, "err", err)
		}
	}()
}

type hashCounter struct {
	h interface {
		io.Writer
		Sum([]byte) []byte
	}
	n int64
}

func (h *hashCounter) Write(p []byte) (int, error) {
	h.n += int64(len(p))
	return h.h.Write(p)
}

func (s *Server) runOut(ctx context.Context, m *store.MoveOut) error {
	u, err := s.store.GetUser(ctx, m.UserID)
	if err != nil {
		return err
	}
	st, _ := s.store.GetSettings(ctx, m.UserID)
	m.Status, m.Results = "running", nil
	for _, a := range m.Apps {
		m.Results = append(m.Results, store.OutAppResult{App: a, Name: appName(a), State: "waiting", Files: []store.OutFile{}})
	}
	_ = s.store.SaveMoveOut(ctx, m)

	dir := s.outDir(m.UserID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	name := fmt.Sprintf("pocket-move-%s-%d.zip", time.Now().Format("2006-01-02"), m.ID)
	final := filepath.Join(dir, name)
	part := final + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	fail := func(err error) error {
		zw.Close()
		f.Close()
		os.Remove(part)
		return err
	}
	top := strings.TrimSuffix(name, ".zip") + "/"
	man := move.Manifest{Format: move.ArchiveFormat, Made: store.Now(), Person: u.Email, Box: s.cfg.BaseURL, Version: Version}

	for i := range m.Results {
		res := &m.Results[i]
		res.State = "running"
		_ = s.store.SaveMoveOut(ctx, m)
		p, err := s.links.Describe(ctx, u.Email, res.App, core.ScopeMoveExport)
		if err != nil {
			res.State, res.Error = "failed", err.Error()
			man.Apps = append(man.Apps, move.ManifestApp{App: res.App, Name: res.Name, Error: res.Error, Files: []move.ManifestFile{}})
			_ = s.store.SaveMoveOut(ctx, m)
			continue
		}
		exports := pickExports(p.Exports, st.OpenFormats)
		if len(exports) == 0 {
			res.State, res.Error = "skipped", res.Name+" has nothing to copy out"
			continue
		}
		ma := move.ManifestApp{App: res.App, Name: res.Name, Files: []move.ManifestFile{}}
		for _, e := range exports {
			rc, err := s.links.Get(ctx, u.Email, res.App, e.Path)
			if err != nil {
				res.Error = err.Error()
				continue
			}
			inner := path.Join(res.App, safeName(e.File))
			hw, err := zw.CreateHeader(&zip.FileHeader{Name: top + inner, Method: zip.Deflate, Modified: time.Now()})
			if err != nil {
				rc.Close()
				return fail(err)
			}
			hc := &hashCounter{h: sha256.New()}
			_, err = io.Copy(io.MultiWriter(hw, hc), rc)
			rc.Close()
			if err != nil {
				// the entry is broken: the archive can't be finished cleanly
				return fail(fmt.Errorf("%s: %w", res.Name, err))
			}
			sum := hex.EncodeToString(hc.h.Sum(nil))
			res.Files = append(res.Files, store.OutFile{Path: inner, Kind: e.Kind, Title: e.Title, Bytes: hc.n, SHA256: sum})
			ma.Files = append(ma.Files, move.ManifestFile{Path: inner, Kind: e.Kind, Title: e.Title, Bytes: hc.n, SHA256: sum})
		}
		if len(res.Files) == 0 {
			res.State = "failed"
		} else {
			res.State = "done"
		}
		man.Apps = append(man.Apps, ma)
		_ = s.store.SaveMoveOut(ctx, m)
	}

	mb, _ := json.MarshalIndent(man, "", "  ")
	if w, err := zw.Create(top + "manifest.json"); err == nil {
		_, _ = w.Write(mb)
	}
	if w, err := zw.Create(top + "README.html"); err == nil {
		_, _ = io.WriteString(w, readmeHTML(man, u.Name))
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, final); err != nil {
		return err
	}
	fi, _ := os.Stat(final)
	m.File, m.Status, m.FinishedAt = final, "done", store.Now()
	if fi != nil {
		m.SizeBytes = fi.Size()
	}
	m.ExpiresAt = time.Now().UTC().Add(time.Duration(s.cfg.KeepDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	ok, failed := 0, 0
	for _, r := range m.Results {
		switch r.State {
		case "done":
			ok++
		case "failed":
			failed++
		}
	}
	if ok == 0 {
		m.Status, m.Error = "failed", "Nothing could be copied out."
		_ = os.Remove(final)
		m.File = ""
	}
	if err := s.store.SaveMoveOut(ctx, m); err != nil {
		return err
	}
	if m.Status == "done" {
		msg := fmt.Sprintf("Your archive is ready: %s, %s.", plural(ok, "app", "apps"), humanBytes(m.SizeBytes))
		if failed > 0 {
			msg += fmt.Sprintf(" %s could not be copied.", plural(failed, "app", "apps"))
		}
		s.notifyDone(ctx, m.UserID, "Moving out", msg, "/out")
		s.publish(ctx, m.UserID, "move.out_ready", fmt.Sprintf("out:%d", m.ID), map[string]any{"apps": ok, "failed": failed, "bytes": m.SizeBytes})
	}
	return nil
}

// pickExports keeps the app's own export and, if wanted, the open-format files.
func pickExports(all []core.PortableExport, open bool) []core.PortableExport {
	var out []core.PortableExport
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.Path] || e.Path == "" || !strings.HasPrefix(e.Path, "/") {
			continue
		}
		if e.Kind == core.KindPocket || open {
			seen[e.Path] = true
			if e.File == "" {
				e.File = path.Base(e.Path)
			}
			out = append(out, e)
		}
	}
	if len(out) == 0 && len(all) > 0 {
		e := all[0]
		if e.File == "" {
			e.File = path.Base(e.Path)
		}
		out = append(out, e)
	}
	return out
}

func safeName(s string) string {
	s = strings.NewReplacer("/", "-", "\\", "-", "..", ".").Replace(strings.TrimSpace(s))
	if s == "" || s == "." {
		return "export"
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

var kindWords = map[string]string{
	core.KindPocket: "The app's own copy: Pocket Move brings it back into the same app on another box.",
	core.KindVCard:  "Contacts (vCard): opens in any address book.", core.KindICalEvents: "Calendar (iCalendar): opens in any calendar app.",
	core.KindICalTodos: "Tasks (iCalendar): opens in Reminders, Thunderbird and most task apps.", core.KindMarkdownZip: "Markdown files: open in any text editor or notes app.",
	core.KindMarkdown: "Markdown: open in any text editor.", core.KindOPMLFeeds: "Feeds (OPML): opens in any feed reader.", core.KindOPMLPodcast: "Podcasts (OPML): opens in any podcast app.",
	core.KindCSV: "A table (CSV): opens in Excel, Numbers or LibreOffice.", core.KindGPX: "GPS tracks (GPX): open in any map or fitness app.", core.KindJSON: "Data (JSON).",
	core.KindZip: "A zip of files.",
}

// readmeHTML is the page at the top of the archive: what is in it, readable without any Pocket app.
func readmeHTML(m move.Manifest, name string) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Your Pocket data</title>
<style>body{font:15px/1.55 system-ui,-apple-system,Segoe UI,sans-serif;max-width:760px;margin:2rem auto;padding:0 1rem;color:#222}h1{font-size:1.6rem}h2{font-size:1.1rem;margin-top:1.8rem}li{margin:.3rem 0}code{background:#f3f0ea;padding:1px 5px;border-radius:4px}.muted{color:#777}@media (prefers-color-scheme:dark){body{background:#17150f;color:#eee}code{background:#2b261d}.muted{color:#aaa}}</style>`)
	who := m.Person
	if name != "" {
		who = name + " (" + m.Person + ")"
	}
	b.WriteString("<h1>Your Pocket data</h1><p>Everything " + esc(who) + " kept in these apps, copied out on " + esc(m.Made[:10]) + ".</p>")
	b.WriteString(`<p class="muted">To move it to another Pocket Box, open Pocket Move there and bring in this zip as it is. Every file also opens without Pocket: see what each one is below. <code>manifest.json</code> lists the files with their checksums.</p>`)
	apps := append([]move.ManifestApp{}, m.Apps...)
	sort.SliceStable(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	for _, a := range apps {
		b.WriteString("<h2>" + esc(a.Name) + "</h2>")
		if a.Error != "" {
			b.WriteString(`<p class="muted">Not copied: ` + esc(a.Error) + "</p>")
			continue
		}
		b.WriteString("<ul>")
		for _, f := range a.Files {
			what := f.Title
			if w := kindWords[f.Kind]; w != "" {
				if what != "" {
					what += ". "
				}
				what += w
			}
			b.WriteString(`<li><a href="` + esc(f.Path) + `"><code>` + esc(f.Path) + "</code></a> · " + esc(humanBytes(f.Bytes)) + "<br>" + esc(what) + "</li>")
		}
		b.WriteString("</ul>")
	}
	b.WriteString("</html>\n")
	return b.String()
}
