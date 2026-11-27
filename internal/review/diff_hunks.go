package review

import (
	"regexp"
	"strconv"
	"strings"
)

// DiffHunk is one hunk of the review patch that adds a line. Body is the hunk without its `@@` header,
// so a hunk that only moved keeps its Body.
type DiffHunk struct {
	File     string
	Header   string
	NewStart int
	Body     string
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// diffHunks splits a unified patch into its hunks and drops each hunk that adds no line,
// because a rule judges only added lines.
func diffHunks(patch string) []DiffHunk {
	var hunks []DiffHunk
	var file string
	var current *DiffHunk
	closeHunk := func() {
		if current != nil && strings.Contains("\n"+current.Body, "\n+") {
			hunks = append(hunks, *current)
		}
		current = nil
	}
	for line := range strings.Lines(patch) {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			closeHunk()
			file = ""
		case current == nil && strings.HasPrefix(line, "+++ "):
			file = patchPath(strings.TrimSuffix(strings.TrimPrefix(line, "+++ "), "\n"))
		case strings.HasPrefix(line, "@@ "):
			closeHunk()
			current = newDiffHunk(file, strings.TrimSuffix(line, "\n"))
		case current != nil:
			current.Body += line
		}
	}
	closeHunk()
	return hunks
}

// patchPath turns the path of a `+++` line into the repo path. Git quotes a path with a tab, a quote, or a
// non-ASCII byte in C style, which Go's string syntax reads.
func patchPath(header string) string {
	unquoted, err := strconv.Unquote(header)
	if err == nil {
		header = unquoted
	}
	return strings.TrimPrefix(header, "b/")
}

// addsLine reports whether line of the new file is a line that the hunk adds.
func (hunk DiffHunk) addsLine(line int) bool {
	current := hunk.NewStart
	for text := range strings.Lines(hunk.Body) {
		switch text[0] {
		case '+':
			if current == line {
				return true
			}
			current++
		case ' ':
			current++
		}
	}
	return false
}

// newDiffHunk returns nil for a hunk of a deleted file or a header it cannot read: such a hunk adds no line.
func newDiffHunk(file string, header string) *DiffHunk {
	match := hunkHeaderPattern.FindStringSubmatch(header)
	if file == "/dev/null" || match == nil {
		return nil
	}
	start, err := strconv.Atoi(match[1])
	if err != nil {
		return nil
	}
	return &DiffHunk{File: file, Header: header, NewStart: start}
}
