package httpapi

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// TestToolchainDriftOnTheConsole: a pinned problem validated on one
// toolchain is flagged once the judge reports another (B1.5, a gcc
// update); a problem whose validation predates the registry is null.
func TestToolchainDriftOnTheConsole(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	a := s.importProblem(owner, "drift-a")
	b := s.importProblem(owner, "drift-b")

	report := func(gcc string) string {
		t.Helper()
		resp, out := s.judgeCall("POST", "/api/judge/toolchain", judgeToken, judgeapi.ToolchainReport{Protocol: judgeapi.ProtocolVersion,
			Toolchain: judgeapi.Toolchain{GCC: gcc, Java: "javac 21.0.8", Python: "Python 3.12.3", Node: "v22.20.0", Isolate: "isolate 2.7", Kernel: "6.8.0"}})
		var reply judgeapi.ToolchainReply
		if resp.StatusCode != 200 || json.Unmarshal(out, &reply) != nil {
			t.Fatalf("toolchain report: %d %s", resp.StatusCode, out)
		}
		return reply.Hash
	}
	if resp, out := s.judgeCall("POST", "/api/judge/toolchain", judgeToken, judgeapi.ToolchainReport{Protocol: 1}); resp.StatusCode != 426 {
		t.Errorf("old protocol toolchain report: %d %s", resp.StatusCode, out)
	}
	first := report("g++ 13.3.0")
	// Problem A was validated by a job on the first toolchain.
	if _, err := s.db.Exec(`INSERT INTO jobs (kind, priority, state, payload, created_at, toolchain_id)
		VALUES ('validate', 90, 'done', '{}', 1, (SELECT id FROM toolchains WHERE hash = ?))`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO validations (problem_id, job_id, content_hash, created_at)
		SELECT id, last_insert_rowid(), content_hash, 1 FROM problems WHERE id = ?`, a); err != nil {
		t.Fatal(err)
	}
	cid := s.createContest(owner, "drift", time.Now().Add(72*time.Hour), map[string]int64{"A": a, "B": b}, nil)
	console := func() (map[string]any, []any) {
		t.Helper()
		code, c := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", cid), nil, "")
		if code != 200 {
			t.Fatalf("contest: %d %v", code, c)
		}
		return c, c["problems"].([]any)
	}
	c, probs := console()
	pa, pb := probs[0].(map[string]any), probs[1].(map[string]any)
	jt, _ := c["judge_toolchain"].(map[string]any)
	if jt["gcc"] != "g++ 13.3.0" || jt["first_seen"] == nil {
		t.Errorf("judge toolchain %v", c["judge_toolchain"])
	}
	if pa["toolchain_drift"] != false || pa["validated_toolchain"].(map[string]any)["gcc"] != "g++ 13.3.0" {
		t.Errorf("A before the update: %v", pa)
	}
	if pb["toolchain_drift"] != nil || pb["validated_toolchain"] != nil {
		t.Errorf("B has no recorded toolchain: %v", pb)
	}

	report("g++ 13.3.1")
	_, probs = console()
	if pa := probs[0].(map[string]any); pa["toolchain_drift"] != true {
		t.Errorf("A after a gcc change: %v", pa)
	}
	// Back on the first toolchain, the flag clears.
	report("g++ 13.3.0")
	if _, probs = console(); probs[0].(map[string]any)["toolchain_drift"] != false {
		t.Errorf("A back on its toolchain: %v", probs[0])
	}
}
