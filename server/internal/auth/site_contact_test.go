package auth

import "testing"

func TestValidContactEmail(t *testing.T) {
	for s, want := range map[string]bool{
		"":                           true,
		"podium@college.edu":         true,
		"first.last+tag@college.edu": true,
		"a?cc=b@c.com":               false,
		"a&b@c.com":                  false,
		"a%40b@c.com":                false,
		"Name <a@c.com>":             false,
		"not an address":             false,
	} {
		if got := ValidContactEmail(s); got != want {
			t.Errorf("ValidContactEmail(%q) = %v, want %v", s, got, want)
		}
	}
}
