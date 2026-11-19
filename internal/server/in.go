package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-move/internal/move"
	"github.com/abjelosevic88/pocket-homelab-move/internal/store"
)

func (s *Server) inDir(uid, id int64) string {
	return filepath.Join(s.cfg.FilesDir, fmt.Sprint(uid), "in", fmt.Sprint(id))
}

func (s *Server) handleListIn(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMoveIn(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "moves")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleUpload takes the file as the raw request body (?name=the file's
// name), streams it to disk and starts looking through it.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "upload"
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if r.ContentLength > s.cfg.MaxUploadBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "the file is larger than this box takes ("+humanBytes(s.cfg.MaxUploadBytes)+")")
		return
	}
	m, err := s.store.CreateMoveIn(ctx, uid, name)
	if err != nil {
		s.fail(w, err, "move")
		return
	}
	dir := s.inDir(uid, m.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		s.fail(w, err, "move")
		return
	}
	dst := filepath.Join(dir, "upload"+strings.ToLower(filepath.Ext(name)))
	f, err := os.Create(dst)
	if err != nil {
		s.fail(w, err, "move")
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	n, err := io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil || n == 0 {
		_ = os.RemoveAll(dir)
		_ = s.store.DeleteMoveIn(context.WithoutCancel(ctx), uid, m.ID)
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			writeErr(w, http.StatusRequestEntityTooLarge, "the file is larger than this box takes ("+humanBytes(s.cfg.MaxUploadBytes)+")")
		case n == 0 && err == nil:
			writeErr(w, http.StatusBadRequest, "the file is empty")
		default:
			writeErr(w, http.StatusBadRequest, "the upload stopped: "+err.Error())
		}
		return
	}
	m.File, m.SizeBytes, m.Status = dst, n, "scanning"
	if err := s.store.SaveMoveIn(ctx, m); err != nil {
		s.fail(w, err, "move")
		return
	}
	s.startScan(m)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) startScan(m *store.MoveIn) {
	go func() {
		s.workers <- struct{}{}
		defer func() { <-s.workers }()
		ctx := context.Background()
		if err := s.scanIn(ctx, m); err != nil {
			m.Status, m.Error = "failed", err.Error()
			_ = s.store.SaveMoveIn(ctx, m)
			s.log.Warn("moving in: scan failed", "id", m.ID, "err", err)
		}
	}()
}

// scanIn opens the upload, finds what is in it, converts every part to its
// own file and picks where each goes. The upload itself is removed after.
func (s *Server) scanIn(ctx context.Context, m *store.MoveIn) error {
	dir := s.inDir(m.UserID, m.ID)
	tmp := filepath.Join(dir, "tmp")
	itemsDir := filepath.Join(dir, "items")
	_ = os.MkdirAll(tmp, 0o750)
	_ = os.MkdirAll(itemsDir, 0o750)
	fs, err := move.Open(m.File, m.Name, tmp)
	if err != nil {
		return err
	}
	res, err := move.Scan(fs)
	if err != nil {
		fs.Close()
		return err
	}
	u, err := s.store.GetUser(ctx, m.UserID)
	if err != nil {
		fs.Close()
		return err
	}
	targets := s.importTargets(ctx, u.Email)
	for i, f := range res.Found {
		it := &store.Item{MoveID: m.ID, UserID: m.UserID, Label: f.Label, Detail: f.Detail, Kind: f.Kind, Count: f.Count, FileName: f.FileName,
			Extra: map[string]string{}, Include: f.Manual == "", Manual: f.Manual, Sort: i}
		for k, v := range f.Extra {
			it.Extra[k] = v
		}
		if len(f.Kinds) > 0 {
			it.Extra["_kinds"] = strings.Join(f.Kinds, ",")
		}
		if f.Prefer != "" {
			it.Extra["_prefer"] = f.Prefer
		}
		if f.Manual != "" {
			it.Status = "manual"
		} else if f.Write != nil {
			p := move.TempPath(itemsDir, i, f.FileName)
			sum, size, err := writeItem(p, f.Write)
			if err != nil {
				it.Status, it.Error, it.Include = "failed", "Could not convert: "+err.Error(), false
				_ = os.Remove(p)
			} else {
				it.File, it.SHA256, it.Bytes = p, sum, size
			}
		}
		if it.Manual == "" && it.Status != "failed" {
			s.chooseTarget(it, targets)
			if it.Target == "" {
				it.Include = false
			}
			if at := s.store.SentBefore(ctx, m.UserID, it.Target, it.SHA256, 0); at != "" && it.Target != "" {
				it.Include = false
				it.Status = "done-before"
				it.SentAt = at
			}
		}
		if err := s.store.AddItem(ctx, it); err != nil {
			fs.Close()
			return err
		}
	}
	fs.Close()
	// the original is not needed any more: every part has its own file now
	_ = os.Remove(m.File)
	_ = os.RemoveAll(tmp)
	m.Source, m.SourceName, m.Notes, m.Status = res.Source, res.SourceName, res.Notes, "ready"
	if len(res.Found) == 0 {
		m.Status, m.Error = "failed", "Nothing Move knows was found in this file."
	}
	return s.store.SaveMoveIn(ctx, m)
}

func writeItem(p string, write func(io.Writer) error) (string, int64, error) {
	f, err := os.Create(p)
	if err != nil {
		return "", 0, err
	}
	hc := &hashCounter{h: sha256.New()}
	err = write(io.MultiWriter(f, hc))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hc.h.Sum(nil)), hc.n, nil
}

// target is an app import an item can go to.
type target struct {
	App    string              `json:"app"`
	Name   string              `json:"name"`
	Import core.PortableImport `json:"import"`
}

// importTargets lists every import the person may use, by kind.
func (s *Server) importTargets(ctx context.Context, email string) map[string][]target {
	out := map[string][]target{}
	if !s.linksReady() {
		return out
	}
	views, err := s.appViews(ctx, email)
	if err != nil {
		return out
	}
	for _, v := range views {
		if !v.In.On {
			continue
		}
		for _, imp := range v.Imports {
			out[imp.Kind] = append(out[imp.Kind], target{App: v.Slug, Name: v.Name, Import: imp})
		}
	}
	return out
}

// optionsFor are the imports that take this item, the preferred app first.
func optionsFor(it *store.Item, targets map[string][]target) []target {
	kinds := []string{it.Kind}
	if k := it.Extra["_kinds"]; k != "" {
		for _, x := range strings.Split(k, ",") {
			if x != it.Kind {
				kinds = append(kinds, x)
			}
		}
	}
	prefer := it.Extra["_prefer"]
	var out []target
	seen := map[string]bool{}
	for _, k := range kinds {
		for _, t := range targets[k] {
			// the app's own export only goes back into the same app
			if k == core.KindPocket && t.App != prefer {
				continue
			}
			key := t.App + " " + t.Import.Path + " " + t.Import.Kind
			if !seen[key] {
				seen[key] = true
				out = append(out, t)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].App == prefer, out[j].App == prefer
		if pi != pj {
			return pi
		}
		return false
	})
	return out
}

func (s *Server) chooseTarget(it *store.Item, targets map[string][]target) {
	opts := optionsFor(it, targets)
	if len(opts) == 0 {
		it.Target, it.Path = "", ""
		return
	}
	it.Target, it.Path = opts[0].App, opts[0].Import.Path
	it.Extra["_kind"] = opts[0].Import.Kind
}

// itemView adds what the page needs: where it can go, and why not.
type itemView struct {
	store.Item
	Options []target `json:"options"`
	Hint    string   `json:"hint,omitempty"`
}

func (s *Server) handleGetIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	m, err := s.store.GetMoveIn(ctx, u.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "move")
		return
	}
	items, err := s.store.Items(ctx, u.ID, m.ID)
	if err != nil {
		s.fail(w, err, "move")
		return
	}
	var targets map[string][]target
	var states map[string][2]linkState
	if m.Status == "ready" || m.Status == "done" {
		targets = s.importTargets(ctx, u.Email)
		if s.linksReady() {
			states, _ = s.links.States(ctx, u.Email)
		}
	}
	views := make([]itemView, 0, len(items))
	for i := range items {
		it := &items[i]
		v := itemView{Item: *it, Options: optionsFor(it, targets)}
		if v.Options == nil {
			v.Options = []target{}
		}
		if it.Manual == "" && len(v.Options) == 0 {
			v.Hint = s.noTargetHint(it, states)
		}
		v.Extra = publicExtra(it.Extra)
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"move": m, "items": views, "core": s.linksReady()})
}

func publicExtra(e map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range e {
		if !strings.HasPrefix(k, "_") {
			out[k] = v
		}
	}
	return out
}

// noTargetHint says why nothing takes an item and what would fix it.
func (s *Server) noTargetHint(it *store.Item, states map[string][2]linkState) string {
	if !s.linksReady() {
		return "Needs Pocket Core to reach your apps. You can still download the converted file."
	}
	prefer := it.Extra["_prefer"]
	if prefer == "" {
		return "No app on this box takes this yet. You can download the converted file."
	}
	name := appName(prefer)
	if st, ok := states[prefer]; ok && !st[1].On {
		return st[1].Reason + "."
	}
	if _, ok := states[prefer]; !ok && states != nil {
		return name + " is not on this box."
	}
	return name + " can't take this yet: it needs an update."
}

func (s *Server) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	it, err := s.store.GetItem(ctx, u.ID, idParam(r, "item"))
	if err != nil || it.MoveID != idParam(r, "id") {
		s.fail(w, store.ErrNotFound, "item")
		return
	}
	var in struct {
		Include *bool             `json:"include"`
		Target  *string           `json:"target"` // "app path kind"
		Extra   map[string]string `json:"extra"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if it.Manual != "" {
		writeErr(w, http.StatusConflict, "this one is done by hand")
		return
	}
	if in.Target != nil {
		if *in.Target == "" {
			it.Target, it.Path = "", ""
		} else {
			found := false
			for _, t := range optionsFor(it, s.importTargets(ctx, u.Email)) {
				if t.App+" "+t.Import.Path+" "+t.Import.Kind == *in.Target {
					it.Target, it.Path, it.Extra["_kind"], found = t.App, t.Import.Path, t.Import.Kind, true
				}
			}
			if !found {
				writeErr(w, http.StatusBadRequest, "that app can't take this")
				return
			}
		}
	}
	for k, v := range in.Extra {
		if (k == "list" || k == "account" || k == "calendar") && len(v) <= 120 {
			it.Extra[k] = strings.TrimSpace(v)
		}
	}
	if in.Include != nil {
		it.Include = *in.Include && it.Target != "" && it.File != ""
	}
	if it.Target == "" {
		it.Include = false
	}
	if err := s.store.SaveItem(ctx, it); err != nil {
		s.fail(w, err, "item")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleDownloadItem(w http.ResponseWriter, r *http.Request) {
	it, err := s.store.GetItem(r.Context(), actorID(r.Context()), idParam(r, "item"))
	if err != nil || it.MoveID != idParam(r, "id") || it.File == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	f, err := os.Open(it.File)
	if err != nil {
		writeErr(w, http.StatusGone, "the file was removed")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(it.FileName, `"`, "'")+`"`)
	http.ServeContent(w, r, it.FileName, st.ModTime(), f)
}

func (s *Server) handleDeleteIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMoveIn(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "move")
		return
	}
	if m.Status == "running" || m.Status == "scanning" || m.Status == "uploading" {
		writeErr(w, http.StatusConflict, "wait until Move is done with it")
		return
	}
	_ = os.RemoveAll(s.inDir(uid, m.ID))
	if err := s.store.DeleteMoveIn(ctx, uid, m.ID); err != nil {
		s.fail(w, err, "move")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunIn sends every chosen item to its app, in the background.
func (s *Server) handleRunIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMoveIn(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "move")
		return
	}
	if m.Status != "ready" && m.Status != "done" {
		writeErr(w, http.StatusConflict, "not ready yet")
		return
	}
	if !s.linksReady() {
		writeErr(w, http.StatusConflict, "Bringing data into your apps needs Pocket Core")
		return
	}
	m.Status, m.Error = "running", ""
	if err := s.store.SaveMoveIn(ctx, m); err != nil {
		s.fail(w, err, "move")
		return
	}
	go func() {
		s.workers <- struct{}{}
		defer func() { <-s.workers }()
		s.runIn(context.Background(), m)
	}()
	writeJSON(w, http.StatusAccepted, m)
}

// sendResult is the numbers from an app's answer, for the page.
func sendSummary(b []byte) json.RawMessage {
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range doc {
		switch x := v.(type) {
		case float64:
			out[k] = x
		case []any:
			if k == "skipped_kinds" || k == "warnings" {
				out[k] = x
			}
		case string:
			if k == "message" || k == "folder" || k == "list" || k == "calendar" || k == "account" {
				out[k] = x
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	j, _ := json.Marshal(out)
	return j
}

func (s *Server) runIn(ctx context.Context, m *store.MoveIn) {
	u, err := s.store.GetUser(ctx, m.UserID)
	if err != nil {
		return
	}
	items, _ := s.store.Items(ctx, m.UserID, m.ID)
	sent, failed := 0, 0
	for i := range items {
		it := &items[i]
		if !it.Include || it.Target == "" || it.File == "" || it.Status == "sent" {
			continue
		}
		p, err := s.links.Describe(ctx, u.Email, it.Target, core.ScopeMoveImport)
		var imp *core.PortableImport
		if err == nil {
			for j, x := range p.Imports {
				if x.Path == it.Path && (x.Kind == it.Extra["_kind"] || it.Extra["_kind"] == "") {
					imp = &p.Imports[j]
					break
				}
			}
			if imp == nil {
				err = fmt.Errorf("%s no longer takes this", appName(it.Target))
			}
		}
		if err == nil && imp.MaxBytes > 0 && it.Bytes > imp.MaxBytes {
			err = fmt.Errorf("%s takes files up to %s; this one is %s", appName(it.Target), humanBytes(imp.MaxBytes), humanBytes(it.Bytes))
		}
		if err == nil {
			var code int
			var body []byte
			code, body, err = s.links.Send(ctx, u.Email, it.Target, *imp, it.File, publicExtra(it.Extra))
			if err == nil && (code < 200 || code > 299) {
				err = fmt.Errorf("%s answered: %s", appName(it.Target), errText(body))
				if strings.TrimSpace(errText(body)) == "" {
					err = fmt.Errorf("%s answered HTTP %d", appName(it.Target), code)
				}
			}
			if err == nil {
				it.Result = sendSummary(body)
			}
		}
		if err != nil {
			it.Status, it.Error = "failed", err.Error()
			failed++
		} else {
			it.Status, it.Error, it.SentAt = "sent", "", store.Now()
			sent++
		}
		_ = s.store.SaveItem(ctx, it)
	}
	m.Status, m.FinishedAt = "done", store.Now()
	if failed > 0 && sent == 0 {
		m.Error = "Nothing could be brought in."
	}
	_ = s.store.SaveMoveIn(ctx, m)
	msg := fmt.Sprintf("%s brought in.", plural(sent, "part", "parts"))
	if failed > 0 {
		msg += fmt.Sprintf(" %s did not work, see Pocket Move.", plural(failed, "part", "parts"))
	}
	s.notifyDone(ctx, m.UserID, "Moving in: "+m.Name, msg, fmt.Sprintf("/in/%d", m.ID))
	s.publish(ctx, m.UserID, "move.in_finished", fmt.Sprintf("in:%d", m.ID), map[string]any{"source": m.Source, "sent": sent, "failed": failed})
}

// resumeAndClean picks up work a restart interrupted and removes old files.
func (s *Server) resumeAndClean(ctx context.Context) {
	if list, err := s.store.UnfinishedMoveOut(ctx); err == nil {
		for i := range list {
			m := list[i]
			m.Status, m.Error, m.FinishedAt = "failed", "Pocket Move restarted while making this archive. Make it again.", store.Now()
			_ = s.store.SaveMoveOut(ctx, &m)
		}
	}
	if list, err := s.store.UnfinishedMoveIn(ctx); err == nil {
		for i := range list {
			m := list[i]
			switch m.Status {
			case "running":
				m.Status = "ready" // items keep their state; Send again picks up the rest
			default:
				m.Status, m.Error = "failed", "Pocket Move restarted while reading this file. Bring it in again."
			}
			_ = s.store.SaveMoveIn(ctx, &m)
		}
	}
	s.clean(ctx)
}

// clean removes archives past their time and the files of old move-ins.
func (s *Server) clean(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if list, err := s.store.ExpiredMoveOut(ctx, now); err == nil {
		for i := range list {
			m := list[i]
			_ = os.Remove(m.File)
			m.File = ""
			m.Status = "expired"
			_ = s.store.SaveMoveOut(ctx, &m)
		}
	}
	before := time.Now().UTC().Add(-time.Duration(s.cfg.KeepDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	if list, err := s.store.OldMoveIn(ctx, before); err == nil {
		for i := range list {
			m := list[i]
			_ = os.RemoveAll(s.inDir(m.UserID, m.ID))
			m.File = ""
			if m.Status == "ready" {
				m.Status = "expired"
			}
			_ = s.store.SaveMoveIn(ctx, &m)
		}
	}
}
