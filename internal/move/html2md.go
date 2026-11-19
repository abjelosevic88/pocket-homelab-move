package move

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// mediaFunc turns an embedded object (Evernote's <en-media hash type>) into Markdown.
type mediaFunc func(attrs map[string]string) string

// HTMLToMarkdown converts note HTML (Evernote ENML, Apple Notes, Keep HTML)
// to readable Markdown: headings, emphasis, links, images, lists (nested),
// check boxes, quotes, code, rules and simple tables. Anything else keeps its text.
func HTMLToMarkdown(src string, media mediaFunc) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}
	c := &conv{media: media}
	c.block(doc, 0)
	out := c.sb.String()
	out = multiBlank.ReplaceAllString(out, "\n\n")
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var (
	multiBlank = regexp.MustCompile(`\n{3,}`)
	spaces     = regexp.MustCompile(`[ \t\r\n\f]+`)
)

type conv struct {
	sb    strings.Builder
	media mediaFunc
}

func attrs(n *html.Node) map[string]string {
	m := map[string]string{}
	for _, a := range n.Attr {
		m[strings.ToLower(a.Key)] = a.Val
	}
	return m
}

func (c *conv) nl(k int) {
	s := c.sb.String()
	have := len(s) - len(strings.TrimRight(s, "\n"))
	if len(s) == 0 {
		return
	}
	for i := have; i < k; i++ {
		c.sb.WriteByte('\n')
	}
}

func isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.P, atom.Div, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Li,
		atom.Blockquote, atom.Pre, atom.Hr, atom.Table, atom.Tr, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Body, atom.Html:
		return true
	}
	return n.Data == "en-note"
}

// block writes block-level children; inline runs are gathered into paragraphs.
func (c *conv) block(n *html.Node, depth int) {
	var inline []*html.Node
	flush := func() {
		if len(inline) == 0 {
			return
		}
		var sb strings.Builder
		for _, x := range inline {
			c.inline(&sb, x)
		}
		text := strings.TrimSpace(sb.String())
		inline = nil
		if text != "" {
			c.sb.WriteString(text)
			c.nl(2)
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && (ch.DataAtom == atom.Head || ch.DataAtom == atom.Script || ch.DataAtom == atom.Style || ch.DataAtom == atom.Title) {
			continue
		}
		if !isBlock(ch) {
			inline = append(inline, ch)
			continue
		}
		flush()
		c.element(ch, depth)
	}
	flush()
}

func (c *conv) element(n *html.Node, depth int) {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		var sb strings.Builder
		c.inlineChildren(&sb, n)
		if t := strings.TrimSpace(sb.String()); t != "" {
			c.nl(2)
			c.sb.WriteString(strings.Repeat("#", level) + " " + t)
			c.nl(2)
		}
	case atom.Ul, atom.Ol:
		c.nl(1)
		i := 0
		for li := n.FirstChild; li != nil; li = li.NextSibling {
			if li.Type != html.ElementNode {
				continue
			}
			i++
			marker := "- "
			if n.DataAtom == atom.Ol {
				marker = itoa(i) + ". "
			}
			c.listItem(li, depth, marker)
		}
		c.nl(2)
	case atom.Li:
		c.listItem(n, depth, "- ")
	case atom.Blockquote:
		inner := &conv{media: c.media}
		inner.block(n, 0)
		text := strings.TrimSpace(inner.sb.String())
		c.nl(2)
		for _, l := range strings.Split(text, "\n") {
			c.sb.WriteString(strings.TrimRight("> "+l, " ") + "\n")
		}
		c.nl(2)
	case atom.Pre:
		c.nl(2)
		c.sb.WriteString("```\n" + strings.Trim(textOf(n), "\n") + "\n```")
		c.nl(2)
	case atom.Hr:
		c.nl(2)
		c.sb.WriteString("---")
		c.nl(2)
	case atom.Table:
		c.table(n)
	default: // p, div, section, en-note, body…
		if n.DataAtom == atom.Div && depth == 0 && onlyInline(n) {
			// Evernote and Apple Notes write one <div> per line
			var sb strings.Builder
			c.inlineChildren(&sb, n)
			t := strings.TrimSpace(sb.String())
			if t == "" {
				c.nl(2)
				return
			}
			c.sb.WriteString(t)
			c.nl(1)
			return
		}
		c.block(n, depth)
		c.nl(2)
	}
}

func onlyInline(n *html.Node) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if isBlock(ch) {
			return false
		}
	}
	return true
}

func (c *conv) listItem(li *html.Node, depth int, marker string) {
	indent := strings.Repeat("  ", depth)
	var sb strings.Builder
	var nested []*html.Node
	for ch := li.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && (ch.DataAtom == atom.Ul || ch.DataAtom == atom.Ol) {
			nested = append(nested, ch)
			continue
		}
		if isBlock(ch) {
			c.inlineChildren(&sb, ch)
			sb.WriteString(" ")
			continue
		}
		c.inline(&sb, ch)
	}
	text := strings.TrimSpace(sb.String())
	if strings.HasPrefix(text, "[ ] ") || strings.HasPrefix(text, "[x] ") {
		marker = "- "
	}
	c.sb.WriteString(indent + marker + text + "\n")
	for _, n := range nested {
		c.element(n, depth+1)
	}
}

func (c *conv) table(t *html.Node) {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Tr {
			var row []string
			for td := n.FirstChild; td != nil; td = td.NextSibling {
				if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
					var sb strings.Builder
					c.inlineChildren(&sb, td)
					row = append(row, strings.ReplaceAll(strings.TrimSpace(sb.String()), "|", "\\|"))
				}
			}
			rows = append(rows, row)
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(t)
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	c.nl(2)
	for i, r := range rows {
		for len(r) < cols {
			r = append(r, "")
		}
		c.sb.WriteString("| " + strings.Join(r, " | ") + " |\n")
		if i == 0 {
			c.sb.WriteString("|" + strings.Repeat(" --- |", cols) + "\n")
		}
	}
	c.nl(2)
}

func (c *conv) inlineChildren(sb *strings.Builder, n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.inline(sb, ch)
	}
}

func (c *conv) inline(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(escapeMD(spaces.ReplaceAllString(n.Data, " ")))
		return
	case html.ElementNode:
	default:
		c.inlineChildren(sb, n)
		return
	}
	wrap := func(mark string) {
		var in strings.Builder
		c.inlineChildren(&in, n)
		t := in.String()
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			sb.WriteString(t)
			return
		}
		lead := t[:len(t)-len(strings.TrimLeft(t, " "))]
		trail := t[len(strings.TrimRight(t, " ")):]
		sb.WriteString(lead + mark + trimmed + mark + trail)
	}
	switch {
	case n.DataAtom == atom.B || n.DataAtom == atom.Strong:
		wrap("**")
	case n.DataAtom == atom.I || n.DataAtom == atom.Em:
		wrap("*")
	case n.DataAtom == atom.S || n.DataAtom == atom.Strike || n.DataAtom == atom.Del:
		wrap("~~")
	case n.DataAtom == atom.Code:
		sb.WriteString("`" + textOf(n) + "`")
	case n.DataAtom == atom.Br:
		sb.WriteString("  \n")
	case n.DataAtom == atom.A:
		a := attrs(n)
		var in strings.Builder
		c.inlineChildren(&in, n)
		t := strings.TrimSpace(in.String())
		href := a["href"]
		switch {
		case href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:"):
			sb.WriteString(t)
		case t == "" || t == href:
			sb.WriteString("<" + href + ">")
		default:
			sb.WriteString("[" + t + "](" + urlPath(href) + ")")
		}
	case n.DataAtom == atom.Img:
		a := attrs(n)
		if a["src"] != "" && !strings.HasPrefix(a["src"], "data:") {
			sb.WriteString("![" + a["alt"] + "](" + urlPath(a["src"]) + ")")
		}
	case n.Data == "en-todo":
		// ENML writes <en-todo/>, which an HTML parser opens and keeps open:
		// whatever follows ends up inside it, so it is written after the box
		if strings.EqualFold(attrs(n)["checked"], "true") {
			sb.WriteString("[x] ")
		} else {
			sb.WriteString("[ ] ")
		}
		c.inlineChildren(sb, n)
	case n.Data == "en-media":
		if c.media != nil {
			sb.WriteString(c.media(attrs(n)))
		}
		c.inlineChildren(sb, n)
	case n.Data == "en-crypt":
		sb.WriteString("*(encrypted text, not converted)*")
	case n.DataAtom == atom.Input && strings.EqualFold(attrs(n)["type"], "checkbox"):
		if _, ok := attrs(n)["checked"]; ok {
			sb.WriteString("[x] ")
		} else {
			sb.WriteString("[ ] ")
		}
	default:
		if isBlock(n) {
			var in strings.Builder
			c.inlineChildren(&in, n)
			sb.WriteString(" " + in.String() + " ")
			return
		}
		c.inlineChildren(sb, n)
	}
}

func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Br {
			sb.WriteString("\n")
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
		if n.Type == html.ElementNode && (n.DataAtom == atom.Div || n.DataAtom == atom.P) {
			sb.WriteString("\n")
		}
	}
	walk(n)
	return sb.String()
}

// escapeMD keeps plain text from turning into Markdown by accident (only the
// characters that matter mid-line).
func escapeMD(s string) string {
	return strings.NewReplacer("*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`).Replace(s)
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
