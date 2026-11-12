package problems

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"testing"
)

// choicesFixture is the file the editor's splitStarter reads too, so the two
// readers cannot drift.
const choicesFixture = "../../../web/tests/fixtures/choices.json"

type choicesCase struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	Source   string `json:"source"`
	Shared   string `json:"shared"`
	Forms    []struct {
		Name string `json:"name"`
		Code string `json:"code"`
	} `json:"forms"`
	Error string `json:"error"`
}

func readChoicesFixture(t *testing.T) []choicesCase {
	t.Helper()
	b, err := os.ReadFile(choicesFixture)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []choicesCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) < 15 {
		t.Fatalf("only %d cases", len(f.Cases))
	}
	return f.Cases
}

// SplitStarter gives the fixture's shared text and forms, or the fixture's
// sentence, for every Java case.
func TestSplitStarterFixture(t *testing.T) {
	for _, c := range readChoicesFixture(t) {
		if c.Language != "java21" {
			continue
		}
		got, err := SplitStarter(c.Source)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("%s: error %v, want %q", c.Name, err, c.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if got.Shared != c.Shared || len(got.Forms) != len(c.Forms) {
			t.Errorf("%s: shared %q with %d forms, want %q with %d", c.Name, got.Shared, len(got.Forms), c.Shared, len(c.Forms))
			continue
		}
		for i, f := range c.Forms {
			if got.Forms[i].Name != f.Name || got.Forms[i].Code != f.Code {
				t.Errorf("%s: form %d is %+v, want %+v", c.Name, i, got.Forms[i], f)
			}
		}
	}
}

// Saving a function problem reads the Java starter: a refused case is a field
// error on starters with the fixture's sentence, anything else is stored byte
// for byte, in any language (only Java is read).
func TestStarterChoicesAreCheckedAtSave(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Choices", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range readChoicesFixture(t) {
		if c.Source == "" {
			continue // an empty starter is the reader's case; a function problem needs one
		}
		e := functionEdit(p)
		top, bottom, stubs := maps.Clone(*e.LockedTop), maps.Clone(*e.LockedBottom), maps.Clone(*e.Starters)
		top["java21"], bottom["java21"], stubs["java21"] = "import java.util.*;\n", "public class Main {}\n", "class Solution {}\n"
		stubs[c.Language] = c.Source
		e.LockedTop, e.LockedBottom, e.Starters = &top, &bottom, &stubs
		var fields FieldErrors
		got, err := s.Update(ctx, p.ID, e)
		if c.Error != "" {
			if !errors.As(err, &fields) || fields["starters"] != c.Error {
				t.Errorf("%s: got %v, want starters: %q", c.Name, err, c.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if got.Starters[c.Language] != c.Source {
			t.Errorf("%s: saved %q, want it unchanged", c.Name, got.Starters[c.Language])
		}
	}
}
