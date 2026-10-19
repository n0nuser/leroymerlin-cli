package leroymerlin

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// The storefront server-renders JSON islands inside <script> tags: one per
// page for the guidance data, and one per product card for tracking. These
// helpers read them without pulling in an HTML parser.

var scriptOpenRe = regexp.MustCompile(`(?is)<script\b[^>]*>`)

// scriptsJSON returns the payload of every <script> tag whose attributes
// contain attr, in document order.
func scriptsJSON(body []byte, attr string) [][]byte {
	var out [][]byte
	for off := 0; off < len(body); {
		loc := scriptOpenRe.FindIndex(body[off:])
		if loc == nil {
			return out
		}
		tagStart, tagEnd := off+loc[0], off+loc[1]
		end := bytes.Index(body[tagEnd:], []byte("</script>"))
		if end < 0 {
			return out
		}
		contentEnd := tagEnd + end
		if bytes.Contains(body[tagStart:tagEnd], []byte(attr)) {
			out = append(out, body[tagEnd:contentEnd])
		}
		off = contentEnd + len("</script>")
	}
	return out
}

// scriptJSON returns the payload of the first <script> tag matching attr.
func scriptJSON(body []byte, attr string) ([]byte, bool) {
	all := scriptsJSON(body, attr)
	if len(all) == 0 {
		return nil, false
	}
	return all[0], true
}

// dataTmsValue decodes class="dataTms" islands and returns the value of every
// entry named name, in document order.
func dataTmsValue(body []byte, name string) []json.RawMessage {
	var out []json.RawMessage
	for _, raw := range scriptsJSON(body, `class="dataTms"`) {
		var entries []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(raw), &entries); err != nil {
			continue
		}
		for _, e := range entries {
			if e.Name == name {
				out = append(out, e.Value)
			}
		}
	}
	return out
}

var classTextRe = regexp.MustCompile(`(?is)class="([^"]*)"[^>]*>([^<]*)<`)

// htmlClassText returns the trimmed text of the first element with the given
// class attribute, if the element has no child tags.
func htmlClassText(body []byte, class string) string {
	for _, m := range classTextRe.FindAllSubmatch(body, -1) {
		for _, have := range strings.Fields(string(m[1])) {
			if have == class {
				return strings.Join(strings.Fields(string(m[2])), " ")
			}
		}
	}
	return ""
}

// htmlAttr returns the value of an attribute on the first tag that carries
// it, e.g. data-main-store-id on the page root component. The markup served by
// the site spells attribute names in lower case.
func htmlAttr(body []byte, name string) string {
	needle := []byte(name + `="`)
	for off := 0; off < len(body); {
		i := bytes.Index(body[off:], needle)
		if i < 0 {
			return ""
		}
		i += off
		if i == 0 || body[i-1] == ' ' || body[i-1] == '\t' || body[i-1] == '\n' || body[i-1] == '\r' {
			start := i + len(needle)
			if end := bytes.IndexByte(body[start:], '"'); end >= 0 {
				return string(body[start : start+end])
			}
		}
		off = i + len(needle)
	}
	return ""
}
