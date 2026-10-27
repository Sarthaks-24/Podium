package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// TestToolchainReportIsAnswered: the judge's toolchain report is checked
// and answered with its hash; v3 stores nothing (the registry went), and
// an older protocol is refused with 426.
func TestToolchainReportIsAnswered(t *testing.T) {
	s := newSite(t)
	tc := judgeapi.Toolchain{GCC: "g++ 13.3.0", Java: "javac 21.0.8", Python: "Python 3.12.3", Node: "v22.20.0", Isolate: "isolate 2.7", Kernel: "6.8.0"}
	resp, out := s.judgeCall("POST", "/api/judge/toolchain", judgeToken, judgeapi.ToolchainReport{Protocol: judgeapi.ProtocolVersion, Toolchain: tc})
	var reply judgeapi.ToolchainReply
	if resp.StatusCode != 200 || json.Unmarshal(out, &reply) != nil || reply.Hash != tc.Hash() {
		t.Fatalf("toolchain report: %d %s", resp.StatusCode, out)
	}
	if resp, out := s.judgeCall("POST", "/api/judge/toolchain", judgeToken, judgeapi.ToolchainReport{Protocol: 1}); resp.StatusCode != 426 {
		t.Errorf("old protocol toolchain report: %d %s", resp.StatusCode, out)
	}
}
