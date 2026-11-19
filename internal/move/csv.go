package move

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
)

// csvHeader reads the header row of a CSV (comma, semicolon or tab).
func csvHeader(head []byte) ([]string, rune) {
	line := firstLine(head)
	sep := ','
	if strings.Count(line, ";") > strings.Count(line, ",") {
		sep = ';'
	}
	if strings.Count(line, "\t") > strings.Count(line, string(sep)) {
		sep = '\t'
	}
	r := csv.NewReader(strings.NewReader(line))
	r.Comma = sep
	r.LazyQuotes = true
	rec, err := r.Read()
	if err != nil {
		return nil, sep
	}
	for i := range rec {
		rec[i] = strings.ToLower(strings.TrimSpace(rec[i]))
	}
	return rec, sep
}

func hasCols(h []string, want ...string) bool {
	set := map[string]bool{}
	for _, c := range h {
		set[c] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func anyCol(h []string, words ...string) bool {
	for _, c := range h {
		for _, w := range words {
			if c == w || strings.Contains(c, w) {
				return true
			}
		}
	}
	return false
}

// csvKind tells what a CSV is from its header.
type csvKind int

const (
	csvUnknown csvKind = iota
	csvGoodreads
	csvStoryGraph
	csvYouTube
	csvMapsList
	csvPasswords
	csvBank
)

func sniffCSV(head []byte) csvKind {
	h, _ := csvHeader(head)
	switch {
	case len(h) == 0:
		return csvUnknown
	case hasCols(h, "book id", "title", "author"):
		return csvGoodreads
	case hasCols(h, "title", "authors", "read status"):
		return csvStoryGraph
	case hasCols(h, "channel id", "channel url", "channel title"):
		return csvYouTube
	case hasCols(h, "title", "note", "url") && len(h) <= 5:
		return csvMapsList
	case anyCol(h, "password") && anyCol(h, "url", "login_uri", "website", "username", "login"):
		return csvPasswords
	case anyCol(h, "date", "datum", "booking", "valuta", "posted", "transaction") &&
		anyCol(h, "amount", "betrag", "iznos", "debit", "credit", "withdrawal", "deposit", "paid out", "paid in", "value", "sum"):
		return csvBank
	}
	return csvUnknown
}

// countRows counts data rows of a CSV (the header excluded).
func countRows(r io.Reader) int {
	cr := csv.NewReader(r)
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	n := -1
	for {
		_, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if _, ok := err.(*csv.ParseError); ok {
				n++
				continue
			}
			break
		}
		n++
	}
	if n < 0 {
		return 0
	}
	return n
}

func countBytes(r io.Reader, sep []byte) int {
	n := 0
	buf := make([]byte, 256*1024)
	carry := []byte{}
	for {
		k, err := r.Read(buf)
		chunk := append(carry, buf[:k]...)
		n += bytes.Count(chunk, sep)
		if len(chunk) >= len(sep)-1 {
			carry = append([]byte{}, chunk[len(chunk)-(len(sep)-1):]...)
		}
		if err != nil {
			return n
		}
	}
}
