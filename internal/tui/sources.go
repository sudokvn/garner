package tui

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/sudokvn/garner/internal/api"
)

const licenceURL = "https://www.gnu.org/licenses/agpl-3.0.html"

// sourceChoices keeps release details off About. Source locations are
// supplied by each build, never inferred from a version or upstream main.
func sourceChoices(version, source string, server *api.Status) *pickList {
	p := &pickList{labelWidth: 12}
	p.items = []pickItem{
		{header: true, label: "Copyright (c) 2026 sudokvn"},
		{header: true, label: "You may modify and redistribute under AGPLv3."},
		{header: true, label: "No warranty; see the licence for its terms."},
		{header: true},
		{label: "Licence", note: "GNU AGPL version 3", value: licenceURL},
	}
	p.choice = len(p.items) - 1
	add := func(label, version, raw string) {
		if link := sourceLink(raw); link != "" {
			p.items = append(p.items, pickItem{label: label, note: clean(version) + " · " + link, value: link})
		} else {
			p.items = append(p.items, pickItem{header: true, label: label + " " + clean(version) + ": no source archive advertised"})
		}
	}
	if server == nil {
		p.items = append(p.items, pickItem{header: true, label: "Server: status unavailable"})
	} else {
		add("Server", server.Version, server.SourceURL)
	}
	add("TUI", version, source)
	return p
}

// A server may advertise its own source location, but must not make the
// browser open a file or another application's protocol. Control characters
// must not reach terminal hyperlinks or the operating system's URL opener.
func sourceLink(raw string) string {
	if strings.ContainsAny(raw, "\\ ") || strings.ContainsFunc(raw, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)
	}) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return ""
	}
	return raw
}
