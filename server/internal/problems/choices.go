package problems

import (
	"errors"
	"regexp"
	"strings"
)

// Form is one way a student may write a Java function: its marker name
// (for example "int[]") and the code under that marker.
type Form struct {
	Name string
	Code string
}

// Choices is a Java starting code split at its choice markers. Shared is the
// text before the first marker, which every form begins with. With no marker
// there is no choice: one form with no name holds the whole source.
type Choices struct {
	Shared string
	Forms  []Form
}

// The sentences a refused starting code gives, in plain words.
const (
	choiceNearMiss = "This looks like a choice line. Write it exactly as // choice: Name."
	choicePair     = "Choose between int[] and ArrayList<Integer>, or between long[] and ArrayList<Long>: one of each."
	choiceThird    = "Give two choices at most: an array and an ArrayList."
	choiceEmpty    = "Put some code under each choice line."
)

// choiceLine finds a line that is meant to be a marker, however it is spelt.
var choiceLine = regexp.MustCompile(`(?i)^\s*//\s*choice\s*:`)

// choicePairs are the element types a problem may offer, each as an array and
// as an ArrayList.
var choicePairs = [][2]string{{"int[]", "ArrayList<Integer>"}, {"long[]", "ArrayList<Long>"}}

// SplitStarter splits a Java starting code at its choice markers. A marker is
// a line that is exactly "// choice: Name" (a trailing carriage return is
// ignored); the text before the first one is shared. Forms and shared text
// keep their bytes. A line that looks like a marker but is not exact, a third
// form, an empty form or a pair other than int[] with ArrayList<Integer> or
// long[] with ArrayList<Long> is refused with a sentence for the editor.
func SplitStarter(src string) (Choices, error) {
	lines := strings.SplitAfter(src, "\n")
	var out Choices
	var cur *Form
	var body strings.Builder
	closeForm := func() {
		if cur != nil {
			cur.Code = body.String()
			out.Forms = append(out.Forms, *cur)
			body.Reset()
		}
	}
	for _, line := range lines {
		bare := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if choiceLine.MatchString(bare) {
			name, ok := strings.CutPrefix(bare, "// choice: ")
			if !ok || name == "" || strings.ContainsAny(name, " \t") {
				return Choices{}, errors.New(choiceNearMiss)
			}
			closeForm()
			cur = &Form{Name: name}
			continue
		}
		if cur == nil {
			out.Shared += line
		} else {
			body.WriteString(line)
		}
	}
	closeForm()
	if len(out.Forms) == 0 {
		return Choices{Forms: []Form{{Code: src}}}, nil
	}
	if len(out.Forms) > 2 {
		return Choices{}, errors.New(choiceThird)
	}
	if !isChoicePair(out.Forms) {
		return Choices{}, errors.New(choicePair)
	}
	for _, f := range out.Forms {
		if strings.TrimSpace(f.Code) == "" {
			return Choices{}, errors.New(choiceEmpty)
		}
	}
	return out, nil
}

// isChoicePair reports whether forms are exactly an array and its ArrayList.
func isChoicePair(forms []Form) bool {
	if len(forms) != 2 {
		return false
	}
	for _, p := range choicePairs {
		if forms[0].Name == p[0] && forms[1].Name == p[1] || forms[0].Name == p[1] && forms[1].Name == p[0] {
			return true
		}
	}
	return false
}
