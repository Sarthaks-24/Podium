package notifications

import (
	"strings"
	"testing"
)

// Every template, in every variant: it says what happened and what to do,
// asking notices carry the reassurance, and no sentence names a person,
// code, evidence or a test, or uses the words the plan rules out.
func TestNoticeTextCarriesNoCodeTestDataOrNames(t *testing.T) {
	forbidden := []string{"flag", "trust", "suspicious", "cheat", "test", "evidence", "source", "code", "output", "input", "rating",
		"score", "@", "Asha", "Ravi", "\u2014", "\u2013", "!"}
	for _, kind := range Kinds {
		for _, outcome := range []string{"", "warning", "zero_problem", "referred"} {
			for _, contest := range []string{"", "Week 6"} {
				for _, label := range []string{"", "B"} {
					for _, upheld := range []bool{false, true} {
						for _, adj := range []bool{false, true} {
							h, todo, re, link := Sentences(kind, contest, label, outcome, upheld, adj)
							if h == "" || todo == "" || link == "" {
								t.Fatalf("%s: empty sentence: %q %q %q", kind, h, todo, link)
							}
							if (kind == CaseShared || kind == Reminder) != (re == Reassurance) {
								t.Errorf("%s: reassurance %q", kind, re)
							}
							for _, s := range []string{h, todo, re} {
								words := " " + strings.ToLower(strings.NewReplacer(".", " ", ",", " ", ":", " ").Replace(s)) + " "
								for _, bad := range forbidden {
									b := strings.ToLower(bad)
									if len(b) > 1 {
										b = " " + b // a word, or the start of one ("tests")
									}
									if strings.Contains(words, b) {
										t.Errorf("%s/%s: %q contains %q", kind, outcome, s, bad)
									}
								}
								if s != "" && !strings.HasSuffix(s, ".") {
									t.Errorf("%s: %q does not end with a full stop", kind, s)
								}
							}
						}
					}
				}
			}
		}
	}
}
