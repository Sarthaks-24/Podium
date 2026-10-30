package worker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// versionLine runs a tool's version command and returns the first line
// it printed (some print to stderr), or "missing" when it cannot run.
func versionLine(ctx context.Context, cmd ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "missing"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line = strings.TrimSpace(line)
	if len(line) > judgeapi.MaxToolchainField {
		line = line[:judgeapi.MaxToolchainField]
	}
	return strings.ToValidUTF8(line, "")
}

// DetectToolchain asks each compiler and runtime the judge uses for its
// version, by the same paths the language table runs, plus isolate and
// the kernel release.
func DetectToolchain(ctx context.Context, isolatePath string) judgeapi.Toolchain {
	langs := judge.Languages
	kernel, err := os.ReadFile("/proc/sys/kernel/osrelease")
	k := strings.TrimSpace(string(kernel))
	if err != nil || k == "" {
		k = "unknown"
	}
	return judgeapi.Toolchain{
		GCC:     versionLine(ctx, langs["cpp17"].Compile[0], "--version"),
		Java:    versionLine(ctx, langs["java21"].Compile[0], "-version"),
		Python:  versionLine(ctx, langs["python3"].Run[0], "--version"),
		Node:    versionLine(ctx, langs["node22"].Run[0], "--version"),
		Isolate: versionLine(ctx, isolatePath, "--version"),
		Kernel:  k,
	}
}
