package download

import (
	"html"
	"regexp"
	"strings"

	"github.com/sudokvn/garner/internal/store"
)

var (
	logBreak        = regexp.MustCompile(`(?i)<br\s*/?\s*>|</(?:p|div)>`)
	logTag          = regexp.MustCompile(`(?i)</?(?:a|b|code|div|em|font|i|p|span|strong|u)(?:\s[^>]*)?\s*>`)
	missingArticles = regexp.MustCompile(`(?i)^([1-9][0-9]*) (articles?) (?:were|was) missing[.!]?$`)
)

// logLines reads the actions of a stage of SABnzbd's HTML log as plain
// lines. Redact credentials after decoding the HTML: an escaped query
// separator, &amp;, would hide a credential parameter from the redaction.
// Clients clean terminal control characters before displaying the lines.
func logLines(actions []string) []string {
	lines := []string{}
	for _, action := range actions {
		plain := html.UnescapeString(logTag.ReplaceAllString(logBreak.ReplaceAllString(action, "\n"), ""))
		for _, line := range strings.FieldsFunc(plain, func(r rune) bool { return r == '\n' || r == '\r' }) {
			if line = strings.TrimSpace(line); line != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// failureSummary prefers concrete download evidence to SABnzbd's generic
// incomplete-job message. Other failures keep their original explanation;
// missing articles must not hide a disk, permission or file-check failure.
func failureSummary(d store.Download) string {
	failure := strings.ToLower(d.Failure)
	if d.State != store.Failed || d.Import.State != "" || !strings.HasPrefix(failure, "aborted, cannot be completed") {
		return ""
	}
	for _, stage := range d.Stages {
		if stage.Name != "Download" {
			continue
		}
		for _, line := range logLines(stage.Actions) {
			if match := missingArticles.FindStringSubmatch(line); match != nil {
				return match[1] + " " + strings.ToLower(match[2]) + " missing"
			}
		}
	}
	return ""
}
