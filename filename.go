package stash

import (
	"regexp"
	"strings"
)

// filenamePattern matches the structured filename convention:
//
//	YYYY-MM-DD_Performer.Name1[_Performer2]-Title.With.Dots_Resolution.ext
//
// date, performers and title are captured. Resolution and extension are
// matched but not extracted — a filename's own resolution claim is no more
// trustworthy than Stash's (see Tier), so ParseFilename does not surface it.
var filenamePattern = regexp.MustCompile(
	`^(?P<date>\d{4}[-._]\d{2}[-._]\d{2})_(?P<performers>.*?)-(?P<title>.+)_` +
		`(?:480|540|720|1080|1440|2160|2880|4320|[2468][kK])p?(?:_\d+)?` +
		`\.[A-Za-z0-9]+$`,
)

// ParsedFilename is what ParseFilename extracted from a structured basename.
type ParsedFilename struct {
	Date       string   // "2024-12-15" — normalized to YYYY-MM-DD
	Title      string   // "A Long Scene Title With Words"
	Performers []string // ["Some Performer"]
}

// ParseFilename extracts a date, title and performer names from a basename
// following the convention YYYY-MM-DD_Performers-Title_Resolution.ext. It
// reports false when the name doesn't match that convention at all — most
// filenames simply aren't structured this way, so that is not an error.
//
// Stash has its own scan-time filename parser; this one exists alongside it
// because it additionally handles multiple underscore-separated performers
// and a dashed or dotted title the way one library is actually named.
func ParseFilename(basename string) (ParsedFilename, bool) {
	match := filenamePattern.FindStringSubmatch(basename)
	if match == nil {
		return ParsedFilename{}, false
	}

	groups := map[string]string{}
	for i, name := range filenamePattern.SubexpNames() {
		if name != "" && i < len(match) {
			groups[name] = match[i]
		}
	}

	var pf ParsedFilename

	if d := groups["date"]; d != "" {
		d = strings.ReplaceAll(d, "_", "-")
		d = strings.ReplaceAll(d, ".", "-")
		pf.Date = d
	}

	if t := groups["title"]; t != "" {
		t = strings.ReplaceAll(t, ".", " ")
		pf.Title = strings.TrimSpace(t)
	}

	if p := groups["performers"]; p != "" {
		for _, raw := range strings.Split(p, "_") {
			name := strings.TrimSpace(strings.ReplaceAll(raw, ".", " "))
			if name != "" {
				pf.Performers = append(pf.Performers, name)
			}
		}
	}

	return pf, true
}
