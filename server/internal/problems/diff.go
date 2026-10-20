package problems

import "strings"

// DiffLine is one line of a statement diff: Op is "same", "del" (only in
// the old text) or "add" (only in the new one).
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// maxDiffCells bounds the line comparison: the old and new lines left once
// the common start and end are set aside, multiplied. Statements are at
// most 64 KB, so only a rewrite of a long statement goes over it.
const maxDiffCells = 1 << 20

// LineDiff compares two texts line by line, by the longest common
// subsequence of lines after trimming the lines both share at the start
// and the end. When the middle is too large to compare (more than
// maxDiffCells line pairs) it is reported coarsely: every old line
// deleted, then every new line added, and coarse is true.
func LineDiff(a, b string) (lines []DiffLine, coarse bool) {
	x, y := splitLines(a), splitLines(b)
	lines = []DiffLine{}
	start := 0
	for start < len(x) && start < len(y) && x[start] == y[start] {
		start++
	}
	endX, endY := len(x), len(y)
	for endX > start && endY > start && x[endX-1] == y[endY-1] {
		endX, endY = endX-1, endY-1
	}
	for _, l := range x[:start] {
		lines = append(lines, DiffLine{"same", l})
	}
	mx, my := x[start:endX], y[start:endY]
	if len(mx)*len(my) > maxDiffCells {
		coarse = true
		for _, l := range mx {
			lines = append(lines, DiffLine{"del", l})
		}
		for _, l := range my {
			lines = append(lines, DiffLine{"add", l})
		}
	} else {
		lines = append(lines, lcsDiff(mx, my)...)
	}
	for _, l := range x[endX:] {
		lines = append(lines, DiffLine{"same", l})
	}
	return lines, coarse
}

// splitLines splits on newlines, dropping a final empty line and a
// trailing carriage return on each line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	out := strings.Split(s, "\n")
	for i, l := range out {
		out[i] = strings.TrimSuffix(l, "\r")
	}
	return out
}

// lcsDiff is the classic dynamic-programming line diff of two short
// slices: deletions before additions within each changed run.
func lcsDiff(x, y []string) []DiffLine {
	n, m := len(x), len(y)
	// lcs[i*(m+1)+j] is the length of the longest common subsequence of
	// x[i:] and y[j:].
	lcs := make([]int32, (n+1)*(m+1))
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i*(m+1)+j] = lcs[(i+1)*(m+1)+j+1] + 1
			} else {
				lcs[i*(m+1)+j] = max(lcs[(i+1)*(m+1)+j], lcs[i*(m+1)+j+1])
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, DiffLine{"same", x[i]})
			i, j = i+1, j+1
		case lcs[(i+1)*(m+1)+j] >= lcs[i*(m+1)+j+1]:
			out = append(out, DiffLine{"del", x[i]})
			i++
		default:
			out = append(out, DiffLine{"add", y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{"del", x[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{"add", y[j]})
	}
	return out
}
