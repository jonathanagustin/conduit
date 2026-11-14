package domain

import "sort"

// DiffSnapshots compares a stored base with the current proposal. Equal paths
// are omitted. The result is display data and is not an approval.
func DiffSnapshots(base, current map[string][]byte) []DiffFile {
	names := map[string]struct{}{}
	for name := range base {
		names[name] = struct{}{}
	}
	for name := range current {
		names[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	files := []DiffFile{}
	for _, name := range ordered {
		before, hadBefore := base[name]
		after, hadAfter := current[name]
		switch {
		case hadBefore && hadAfter && string(before) == string(after):
			continue
		case !hadBefore && hadAfter:
			files = append(files, DiffFile{Path: name, Change: "added", Hunks: []Hunk{textHunk("added", after)}})
		case hadBefore && !hadAfter:
			files = append(files, DiffFile{Path: name, Change: "removed", Hunks: []Hunk{textHunk("removed", before)}})
		default:
			files = append(files, DiffFile{Path: name, Change: "changed", Hunks: []Hunk{
				textHunk("removed", before),
				textHunk("added", after),
			}})
		}
	}
	return files
}

func textHunk(kind string, content []byte) Hunk {
	text, _ := RedactText(string(content))
	if len(text) > 4000 {
		text = text[:4000] + "…"
	}
	return Hunk{Header: kind, Lines: splitLines(text)}
}

func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := []string{}
	start := 0
	for index := 0; index <= len(text); index++ {
		if index == len(text) || text[index] == '\n' {
			lines = append(lines, text[start:index])
			start = index + 1
		}
	}
	return lines
}
