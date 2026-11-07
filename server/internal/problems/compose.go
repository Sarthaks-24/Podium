package problems

// Function problems (v2.0.2): the student writes only the middle of the
// file, and the judge compiles the locked top, their code and the locked
// bottom as one source file. Compose is the one place that joins them; the
// editor's join must give the same file (web/tests/fixtures/compose.json is
// read by both tests, V202-34).

import "strings"

// Composed is the file the judge compiles for a function problem, with the
// line offsets that map its lines back to the parts: lines 1 to TopLines
// are the locked top, the next CodeLines are the student's code, the rest
// the locked bottom.
type Composed struct {
	Source              string
	TopLines, CodeLines int
}

// Compose joins the three parts. Each part is cut after its last line (line
// ends and blank lines at its end go) and given one newline; an empty part
// adds nothing. So the student's first line is line TopLines+1, and
// nothing inside a part changes.
func Compose(top, code, bottom string) Composed {
	t, c, b := normalise(top), normalise(code), normalise(bottom)
	return Composed{Source: t + c + b, TopLines: strings.Count(t, "\n"), CodeLines: strings.Count(c, "\n")}
}

// CodeLines is how many lines the student's code takes in the composed
// file, from the code alone, so a stored submission maps without its top.
func CodeLines(code string) int { return strings.Count(normalise(code), "\n") }

// normalise cuts a part after its last line and ends it with one newline.
func normalise(s string) string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// ComposeFor is the file to judge for a student's code in lang, with the
// problem's locked parts as they are now: in a contest, checkPin has made
// sure they are the pinned revision's (PLAN 3.1). A whole problem judges
// the code exactly as sent, with no offsets.
func (p *Problem) ComposeFor(lang, code string) Composed {
	if !p.IsFunction() {
		return Composed{Source: code}
	}
	return Compose(p.LockedTop[lang], code, p.LockedBottom[lang])
}
