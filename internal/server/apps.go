package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// pocketApp is a Pocket app Move can move data out of and into.
type pocketApp struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// pocketApps are every Pocket app on a box (Core's registered slugs). Move
// asks Core for move:export and move:import in each; an app that has not
// adopted Pocket Core's Portable yet answers "not offered".
var pocketApps = []pocketApp{
	{"alerts", "Pocket Alerts"}, {"audiobooks", "Pocket Audiobooks"}, {"backups", "Pocket Backups"}, {"books", "Pocket Balkan Books"},
	{"blog", "Pocket Blog"}, {"blueprint", "Pocket Blueprint"}, {"boards", "Pocket Boards"}, {"booking", "Pocket Booking"},
	{"bookshelf", "Pocket Bookshelf"}, {"calendar", "Pocket Calendar"}, {"cameras", "Pocket Cameras"}, {"capsule", "Pocket Capsule"},
	{"chat", "Pocket Chat"}, {"clips", "Pocket Clips"}, {"contacts", "Pocket Contacts"}, {"crm", "Pocket CRM"},
	{"database", "Pocket Database"}, {"day", "Pocket Day"}, {"documents", "Pocket Documents"}, {"draw", "Pocket Draw"},
	{"errors", "Pocket Errors"}, {"family", "Pocket Family"}, {"files", "Pocket Files"}, {"fitness", "Pocket Fitness"},
	{"flows", "Pocket Flows"}, {"forms", "Pocket Forms"}, {"forum", "Pocket Forum"}, {"gamebuilder", "Pocket Game Builder"},
	{"games", "Pocket Games"}, {"garage", "Pocket Garage"}, {"guitar", "Pocket Guitar"}, {"habits", "Pocket Habits"},
	{"health", "Pocket Health"}, {"home", "Pocket Home"}, {"horoscope", "Pocket Horoscope"}, {"inventory", "Pocket Inventory"},
	{"invoicing", "Pocket Invoicing"}, {"jam", "Pocket Jam"}, {"jobs", "Pocket Jobs"}, {"karaoke", "Pocket Karaoke"},
	{"kitchen", "Pocket Kitchen"}, {"league", "Pocket League"}, {"learn", "Pocket Learn"}, {"library", "Pocket Library"},
	{"lookout", "Pocket Lookout"}, {"mail", "Pocket Mail"}, {"map", "Pocket Map"}, {"math", "Pocket Math"},
	{"money", "Pocket Money"}, {"monitoring", "Pocket Monitoring"}, {"movement", "Pocket Movement"}, {"music", "Pocket Music"},
	{"network", "Pocket Network"}, {"notes", "Pocket Notes"}, {"nutrition", "Pocket Nutrition"}, {"office", "Pocket Office"},
	{"parcels", "Pocket Parcels"}, {"pets", "Pocket Pets"}, {"photolab", "Pocket Photo Lab"}, {"photos", "Pocket Photos"},
	{"places", "Pocket Places"}, {"podcasts", "Pocket Podcasts"}, {"printfarm", "Pocket Print Farm"}, {"projects", "Pocket Projects"},
	{"pr-review", "Pocket PR Review"}, {"puzzlecraft", "Pocket Puzzlecraft"}, {"quest", "Pocket Quest"}, {"radio", "Pocket Radio"},
	{"reader", "Pocket Reader"}, {"remote", "Pocket Remote"}, {"renewals", "Pocket Renewals"}, {"rentals", "Pocket Rentals"},
	{"requests", "Pocket Requests"}, {"review", "Pocket Review"}, {"rooms", "Pocket Rooms"}, {"search", "Pocket Search"},
	{"sign", "Pocket Sign"}, {"sky", "Pocket Sky"}, {"slides", "Pocket Slides"}, {"sphere", "Pocket Sphere"},
	{"split", "Pocket Split"}, {"still", "Pocket Still"}, {"subtitles", "Pocket Subtitles"}, {"tables", "Pocket Tables"},
	{"tasks", "Pocket Tasks"}, {"time", "Pocket Time"}, {"trails", "Pocket Trails"}, {"translate", "Pocket Translate"},
	{"travel", "Pocket Travel"}, {"vault", "Pocket Vault"}, {"voice", "Pocket Voice"}, {"wallet", "Pocket Wallet"},
	{"wardrobe", "Pocket Wardrobe"}, {"watch", "Pocket Watch"}, {"wealth", "Pocket Wealth"}, {"weather", "Pocket Weather"},
	{"whiteboard", "Pocket Whiteboard"}, {"wishlist", "Pocket Wishlist"},
}

func appBySlug(slug string) (pocketApp, bool) {
	for _, a := range pocketApps {
		if a.Slug == slug {
			return a, true
		}
	}
	return pocketApp{}, false
}

func appName(slug string) string {
	if a, ok := appBySlug(slug); ok {
		return a.Name
	}
	return slug
}

// movable are the apps that declare Pocket Core's Portable (move:export /
// move:import). Move asks Core for links only in these, so the apps still
// waiting for that update don't show as broken links in Core; add an app
// here when it adopts Portable.
var movable = map[string]bool{
	"bookshelf": true, "calendar": true, "contacts": true, "day": true, "files": true, "money": true,
	"movement": true, "notes": true, "places": true, "podcasts": true, "reader": true, "tasks": true,
	// 2026-11-20
	"music": true, "office": true, "network": true, "nutrition": true, "parcels": true, "pets": true,
	"photolab": true, "printfarm": true, "projects": true, "pr-review": true,
	"puzzlecraft": true, "quest": true, "radio": true, "remote": true, "renewals": true, "rentals": true,
	"requests": true, "review": true, "rooms": true, "search": true, "sign": true,
	"travel": true, "voice": true, "wallet": true, "wardrobe": true, "watch": true, "wealth": true,
	"weather": true, "whiteboard": true, "wishlist": true,
	"clips": true, "crm": true, "database": true, "documents": true, "draw": true, "errors": true,
	"family": true, "fitness": true, "flows": true, "forms": true, "forum": true,
}

// nothingToMove are apps that keep nothing of a person's own to move, and why.
var nothingToMove = map[string]string{
	"photos": "your photos stay in Immich; Pocket Photos keeps only its connections",
	"vault":  "your vault is encrypted on your devices: export and import it in the Bitwarden apps",
}

// linkUses asks for move:export and move:import in every app that can move.
func linkUses() []core.Use {
	out := make([]core.Use, 0, len(movable))
	for _, a := range pocketApps {
		if !movable[a.Slug] {
			continue
		}
		out = append(out, core.Use{App: a.Slug, Scopes: []string{core.ScopeMoveExport, core.ScopeMoveImport},
			Reason: "Copy your data out of " + a.Name + " into one archive, and bring data in from other apps and other boxes"})
	}
	return out
}

// linkState is one direction (out or in) of Move's link to an app, for one person.
type linkState struct {
	On     bool   `json:"on"`
	State  string `json:"state"` // on | off_admin | off_person | not_offered | target_missing | …
	Reason string `json:"reason,omitempty"`
}

// appLinks is how Move reaches apps for a person: through Pocket Core, or a
// stand-in in demo mode and tests.
type appLinks interface {
	// States lists, per app slug, the move:export and move:import links.
	States(ctx context.Context, email string) (map[string][2]linkState, error)
	// Describe asks an app what it can give and take.
	Describe(ctx context.Context, email, slug, scope string) (*core.Portable, error)
	// Get downloads one export.
	Get(ctx context.Context, email, slug, path string) (io.ReadCloser, error)
	// Send posts a file to an import; the answer's body comes back.
	Send(ctx context.Context, email, slug string, imp core.PortableImport, file string, extra map[string]string) (int, []byte, error)
}

// linkReason puts Core's refusal in plain words.
func linkReason(app, reason string) string {
	switch reason {
	case "off_person":
		return "you switched Pocket Move → " + app + " off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "Pocket Move → " + app + " is switched off in Pocket Core (Links)"
	case "not_offered":
		return app + " can't do this yet: it needs an update"
	case "target_missing":
		return app + " is not on this box"
	case "not_in_audience", "no_account":
		return "you don't use " + app + " on this box"
	case "not_declared":
		return "Pocket Core has not seen this Pocket Move version yet; it re-registers within a minute of starting"
	case "unknown_user":
		return "Pocket Core doesn't know your email address"
	case "", "on":
		return ""
	}
	return reason
}

// ---- through Pocket Core ----

type coreLinks struct {
	c       *core.Client
	http    *http.Client
	mu      sync.Mutex
	descr   map[string]descrCache
	timeout time.Duration
}

type descrCache struct {
	p  *core.Portable
	at time.Time
}

func newCoreLinks(c *core.Client) *coreLinks {
	return &coreLinks{c: c, http: &http.Client{}, descr: map[string]descrCache{}, timeout: 30 * time.Minute}
}

func (l *coreLinks) States(ctx context.Context, email string) (map[string][2]linkState, error) {
	states, err := l.c.LinkStatus(ctx, email, "")
	if err != nil {
		return nil, err
	}
	out := map[string][2]linkState{}
	for _, st := range states {
		pair := out[st.App]
		ls := linkState{On: st.On, State: st.State}
		if !st.On {
			ls.Reason = linkReason(appName(st.App), st.State)
		}
		switch st.Scope {
		case core.ScopeMoveExport:
			pair[0] = ls
		case core.ScopeMoveImport:
			pair[1] = ls
		default:
			continue
		}
		out[st.App] = pair
	}
	return out, nil
}

func linkErr(app string, err error) error {
	var le *core.LinkError
	if errors.As(err, &le) {
		return errors.New(linkReason(appName(app), le.Reason))
	}
	return err
}

func (l *coreLinks) token(ctx context.Context, email, slug, scope string) (*core.LinkToken, error) {
	tok, err := l.c.ActAs(ctx, email, slug, scope)
	if err != nil {
		return nil, linkErr(slug, err)
	}
	return tok, nil
}

func (l *coreLinks) Describe(ctx context.Context, email, slug, scope string) (*core.Portable, error) {
	key := email + "|" + slug + "|" + scope
	l.mu.Lock()
	if c, ok := l.descr[key]; ok && time.Since(c.at) < 2*time.Minute {
		l.mu.Unlock()
		return c.p, nil
	}
	l.mu.Unlock()
	tok, err := l.token(ctx, email, slug, scope)
	if err != nil {
		return nil, err
	}
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, strings.TrimRight(tok.URL, "/")+core.PortablePath, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	res, err := l.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s did not answer: %w", appName(slug), err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%s can't do this yet: it needs an update", appName(slug))
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered HTTP %d", appName(slug), res.StatusCode)
	}
	var p core.Portable
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", appName(slug), err)
	}
	l.mu.Lock()
	l.descr[key] = descrCache{p: &p, at: time.Now()}
	l.mu.Unlock()
	return &p, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }

func (l *coreLinks) Get(ctx context.Context, email, slug, path string) (io.ReadCloser, error) {
	tok, err := l.token(ctx, email, slug, core.ScopeMoveExport)
	if err != nil {
		return nil, err
	}
	rctx, cancel := context.WithTimeout(ctx, l.timeout)
	req, _ := http.NewRequestWithContext(rctx, http.MethodGet, strings.TrimRight(tok.URL, "/")+path, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	res, err := l.http.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s did not answer: %w", appName(slug), err)
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		res.Body.Close()
		cancel()
		return nil, fmt.Errorf("%s answered HTTP %d: %s", appName(slug), res.StatusCode, errText(b))
	}
	return cancelOnClose{res.Body, cancel}, nil
}

func (l *coreLinks) Send(ctx context.Context, email, slug string, imp core.PortableImport, file string, extra map[string]string) (int, []byte, error) {
	tok, err := l.token(ctx, email, slug, core.ScopeMoveImport)
	if err != nil {
		return 0, nil, err
	}
	u := strings.TrimRight(tok.URL, "/") + imp.Path
	body, ctype, size, err := requestBody(imp, file, extra)
	if err != nil {
		return 0, nil, err
	}
	defer body.Close()
	if imp.Form == "" && len(extra) > 0 {
		q := url.Values{}
		for k, v := range extra {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	rctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, http.MethodPost, u, body)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", ctype)
	if size >= 0 {
		req.ContentLength = size
	}
	res, err := l.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s did not answer: %w", appName(slug), err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, b, nil
}

// requestBody streams the file as the raw body or in a multipart form.
func requestBody(imp core.PortableImport, file string, extra map[string]string) (io.ReadCloser, string, int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, "", 0, err
	}
	if imp.Form == "" {
		st, _ := f.Stat()
		ct := imp.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		return f, ct, st.Size(), nil
	}
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer f.Close()
		keys := make([]string, 0, len(extra))
		for k := range extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := mw.WriteField(k, extra[k]); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		fw, err := mw.CreateFormFile(imp.Form, filepath.Base(file))
		if err == nil {
			_, err = io.Copy(fw, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	return pr, mw.FormDataContentType(), -1, nil
}

// errText is the "error" of an app's JSON answer, else the start of the body.
func errText(b []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return e.Error
	}
	s := strings.TrimSpace(string(bytes.ToValidUTF8(b, nil)))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
