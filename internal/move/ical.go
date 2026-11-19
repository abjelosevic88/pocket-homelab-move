package move

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// icsInfo counts what a calendar file holds and its name.
type icsInfo struct {
	Events, Todos int
	Name          string
}

func scanICS(r io.Reader) icsInfo {
	var in icsInfo
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		switch {
		case strings.EqualFold(l, "BEGIN:VEVENT"):
			in.Events++
		case strings.EqualFold(l, "BEGIN:VTODO"):
			in.Todos++
		case in.Name == "" && strings.HasPrefix(strings.ToUpper(l), "X-WR-CALNAME"):
			if i := strings.Index(l, ":"); i > 0 {
				in.Name = unescapeICS(l[i+1:])
			}
		}
	}
	return in
}

func unescapeICS(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

func escapeICS(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
}

// icsWriter writes folded iCalendar lines.
type icsWriter struct{ b bytes.Buffer }

func (w *icsWriter) line(s string) {
	// fold at 75 octets without splitting a UTF-8 sequence
	for len(s) > 75 {
		cut := 75
		for cut > 0 && (s[cut]&0xC0) == 0x80 {
			cut--
		}
		w.b.WriteString(s[:cut] + "\r\n")
		s = " " + s[cut:]
	}
	w.b.WriteString(s + "\r\n")
}

// Google Tasks (Takeout/Tasks/Tasks.json).
type gTaskLists struct {
	Kind  string `json:"kind"`
	Items []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Items []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Notes     string `json:"notes"`
			Status    string `json:"status"` // needsAction | completed
			Due       string `json:"due"`
			Completed string `json:"completed"`
			Updated   string `json:"updated"`
			Created   string `json:"created"`
			Parent    string `json:"parent"`
			Deleted   bool   `json:"deleted"`
			Hidden    bool   `json:"hidden"`
		} `json:"items"`
	} `json:"items"`
}

func isGoogleTasks(head []byte) bool {
	return bytes.Contains(head, []byte(`"tasks#taskLists"`))
}

// TaskList is one Google Tasks list converted to VTODOs.
type TaskList struct {
	Name  string
	Count int
	ICS   []byte
}

func icsTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format("20060102T150405Z")
}

func uidFor(parts ...string) string {
	h := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:12]) + "@pocket-move"
}

// GoogleTasksToICS turns Tasks.json into one VTODO calendar per list
// (X-WR-CALNAME = the list), keeping notes, due dates, completion and subtasks.
func GoogleTasksToICS(data []byte) ([]TaskList, error) {
	var gl gTaskLists
	if err := json.Unmarshal(data, &gl); err != nil {
		return nil, err
	}
	var out []TaskList
	for _, l := range gl.Items {
		w := &icsWriter{}
		w.line("BEGIN:VCALENDAR")
		w.line("VERSION:2.0")
		w.line("PRODID:-//Pocket Move//Google Tasks//EN")
		w.line("X-WR-CALNAME:" + escapeICS(l.Title))
		n := 0
		for _, t := range l.Items {
			if t.Deleted || strings.TrimSpace(t.Title) == "" && strings.TrimSpace(t.Notes) == "" {
				continue
			}
			n++
			w.line("BEGIN:VTODO")
			w.line("UID:" + uidFor("gtask", l.ID, t.ID))
			// the same Tasks.json must give the same file, so Move knows it was brought in before
			stamp := icsTime(t.Updated)
			if stamp == "" {
				stamp = icsTime(t.Created)
			}
			if stamp == "" {
				stamp = "20000101T000000Z"
			}
			w.line("DTSTAMP:" + stamp)
			if c := icsTime(t.Created); c != "" {
				w.line("CREATED:" + c)
			}
			w.line("SUMMARY:" + escapeICS(t.Title))
			if t.Notes != "" {
				w.line("DESCRIPTION:" + escapeICS(t.Notes))
			}
			if t.Due != "" {
				if d, err := time.Parse(time.RFC3339, t.Due); err == nil {
					// Google keeps due dates as dates at midnight UTC
					w.line("DUE;VALUE=DATE:" + d.UTC().Format("20060102"))
				}
			}
			if t.Status == "completed" {
				w.line("STATUS:COMPLETED")
				if c := icsTime(t.Completed); c != "" {
					w.line("COMPLETED:" + c)
				}
			} else {
				w.line("STATUS:NEEDS-ACTION")
			}
			if t.Parent != "" {
				w.line("RELATED-TO;RELTYPE=PARENT:" + uidFor("gtask", l.ID, t.Parent))
			}
			w.line("END:VTODO")
		}
		w.line("END:VCALENDAR")
		if n > 0 {
			out = append(out, TaskList{Name: l.Title, Count: n, ICS: w.b.Bytes()})
		}
	}
	return out, nil
}
