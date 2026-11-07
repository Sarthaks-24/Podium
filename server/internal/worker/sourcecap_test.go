package worker

import (
	"context"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// The judge reads a program source of up to 72 KB: a function problem's
// composed file is the student's 64 KB plus a 4 KB top and a 4 KB bottom
// (V202-39). One byte more is refused.
func TestJudgeReadsAComposedFileOfTheLargestSize(t *testing.T) {
	if judgeapi.MaxProgramSource != 72<<10 {
		t.Fatalf("MaxProgramSource is %d", judgeapi.MaxProgramSource)
	}
	okSHA, ok := blob(judgeapi.MaxProgramSource, 'a')
	bigSHA, big := blob(judgeapi.MaxProgramSource+1, 'a')
	client, _ := blobServer(t, map[string][]byte{okSHA: ok, bigSHA: big})
	c := &Cache{Dir: t.TempDir(), Client: client}
	l := Lease{JobID: 1, Token: "x"}
	if got, err := c.Read(context.Background(), l, okSHA, maxSourceBytes); err != nil || len(got) != len(ok) {
		t.Errorf("72 KB source: %d bytes, %v", len(got), err)
	}
	if _, err := c.Read(context.Background(), l, bigSHA, maxSourceBytes); err == nil {
		t.Error("a source over 72 KB was read")
	}
}
