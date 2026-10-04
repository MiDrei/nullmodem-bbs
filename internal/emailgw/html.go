package emailgw

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// HTMLText is an HTML mail's text: paragraphs and line breaks kept,
// list items with a dash, links with their address, scripts and
// styles left out.
func HTMLText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var b strings.Builder
	skip := 0
	var href string
	var linkText strings.Builder
	inLink := false
	newline := func(n int) {
		s := b.String()
		trail := len(s) - len(strings.TrimRight(s, "\n"))
		for ; trail < n && b.Len() > 0; trail++ {
			b.WriteByte('\n')
		}
	}
	for {
		switch z.Next() {
		case html.ErrorToken:
			return tidy(b.String())
		case html.TextToken:
			if skip > 0 {
				continue
			}
			t := spaces.ReplaceAllString(string(z.Text()), " ")
			if inLink {
				linkText.WriteString(t)
				continue
			}
			if strings.HasSuffix(b.String(), "\n") {
				t = strings.TrimLeft(t, " ")
			}
			b.WriteString(t)
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "script", "style", "head", "title":
				skip++
			case "br":
				b.WriteByte('\n')
			case "p", "div", "table", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "ul", "ol", "hr":
				newline(2)
			case "tr":
				newline(1)
			case "td", "th":
				b.WriteString(" ")
			case "li":
				newline(1)
				b.WriteString("- ")
			case "a":
				href, inLink = "", true
				linkText.Reset()
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					if string(k) == "href" {
						href = string(v)
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "head", "title":
				if skip > 0 {
					skip--
				}
			case "p", "div", "table", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "ul", "ol":
				newline(2)
			case "a":
				inLink = false
				t := strings.TrimSpace(linkText.String())
				b.WriteString(t)
				if strings.HasPrefix(href, "http") && href != t {
					b.WriteString(" <" + href + ">")
				}
			}
		}
	}
}

var (
	spaces    = regexp.MustCompile(`[ \t\r\n\f]+`)
	manyBlank = regexp.MustCompile(`\n{3,}`)
)

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.TrimSpace(manyBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
