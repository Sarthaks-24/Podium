package judgeapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// A v1.0.0 judge sends no protocol field; a judge of this build always
// sends its version, which the server compares exactly (D117, C1-027).
func TestClaimCarriesTheProtocolVersion(t *testing.T) {
	var old ClaimRequest
	if err := json.Unmarshal([]byte(`{"kinds":["submit"],"worker":"vm","version":"v1.0.0","isolate":"2.0"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Protocol != 0 {
		t.Errorf("a claim without the field decodes as %d, want 0 (protocol 1)", old.Protocol)
	}
	b, _ := json.Marshal(ClaimRequest{Kinds: []string{"submit"}, Protocol: ProtocolVersion, Toolchain: "abc"})
	if !strings.Contains(string(b), `"protocol":3`) || ProtocolVersion != 3 {
		t.Errorf("claim %s, protocol %d", b, ProtocolVersion)
	}
	// Version 3 added Reveal; it stays out of a payload that does not reveal.
	b, _ = json.Marshal(SubmitPayload{})
	if strings.Contains(string(b), "reveal") {
		t.Errorf("a payload without a reveal names it: %s", b)
	}
	b, _ = json.Marshal(ToolchainReport{Protocol: ProtocolVersion})
	if !strings.Contains(string(b), `"protocol":3`) {
		t.Errorf("toolchain report %s", b)
	}
}

// The toolchain hash is the SHA-256 of the JSON in field order, so the
// same tools always give the same hash and any change gives another.
func TestToolchainHashAndCheck(t *testing.T) {
	a := Toolchain{GCC: "gcc 13.3.0", Java: "openjdk 21", Python: "Python 3.12.3", Node: "v22.11.0", Isolate: "2.0", Kernel: "6.8.0"}
	if a.Hash() != a.Hash() || len(a.Hash()) != 64 || !ValidSHA(a.Hash()) {
		t.Fatalf("hash %q", a.Hash())
	}
	b := a
	b.Kernel = "6.8.1"
	if a.Hash() == b.Hash() {
		t.Error("a kernel change kept the hash")
	}
	if err := a.Check(); err != nil {
		t.Errorf("a normal report refused: %v", err)
	}
	long := a
	long.GCC = strings.Repeat("x", MaxToolchainField+1)
	if long.Check() == nil {
		t.Error("an oversized field accepted")
	}
	bad := a
	bad.Node = "\xff"
	if bad.Check() == nil {
		t.Error("invalid UTF-8 accepted")
	}
}

func TestValidSHA(t *testing.T) {
	for s, want := range map[string]bool{
		strings.Repeat("a", 64): true, strings.Repeat("A", 64): false, strings.Repeat("a", 63): false, strings.Repeat("g", 64): false, "": false,
	} {
		if ValidSHA(s) != want {
			t.Errorf("ValidSHA(%q) = %v", s, !want)
		}
	}
}
