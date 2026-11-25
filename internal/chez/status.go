package chez

import "strings"

// StatusEntry is one line of chezmoi status.
type StatusEntry struct {
	Last  byte   // change since chezmoi last wrote the target: A, D, M, R or space
	Apply byte   // what chezmoi apply would do: A, D, M, R or space
	Path  string // the target, relative to the destination dir
}

// ParseStatus reads the output of chezmoi status: two status columns, a space and
// the target path. Lines it cannot read are skipped.
func ParseStatus(out string) []StatusEntry {
	var entries []StatusEntry
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimRight(line, "\r")
		e, ok := parseStatusLine(line)
		if ok {
			entries = append(entries, e)
		}
	}
	return entries
}

func statusColumn(b byte) bool {
	switch b {
	case ' ', 'A', 'D', 'M', 'R':
		return true
	}
	return false
}

func parseStatusLine(line string) (StatusEntry, bool) {
	if len(line) < 4 || line[2] != ' ' || !statusColumn(line[0]) || !statusColumn(line[1]) || strings.TrimSpace(line[3:]) == "" {
		return StatusEntry{}, false
	}
	return StatusEntry{Last: line[0], Apply: line[1], Path: line[3:]}, true
}
