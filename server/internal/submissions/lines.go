package submissions

// Line numbers in a function problem's compiler output (PLAN 3.3, V202-33).
// The judge compiles the composed file, so a compiler names lines of that
// file. The student should see lines of their own code: a location inside
// it has the top's line count taken off; a location inside the locked top
// or bottom becomes "locked code, line N", N counted within that block.
// The shapes are the four in web/src/lib/compileErrors.ts, so the web app
// still finds the student's locations in the rewritten text.
//
// Compilers echo the student's source line after a location (javac, Node,
// Python) or behind a "NN |" gutter (g++). Those lines are never searched
// for locations, so code that merely looks like one is left alone.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// lineMap places a composed file's lines: top locked lines, then code lines
// of the student's, then the locked bottom. A zero top means the file was
// not composed (a whole problem; a function problem's top is never empty).
type lineMap struct{ top, code int }

// place returns the line within the student's code, or within the locked
// block when locked is set.
func (m lineMap) place(n int) (line int, locked bool) {
	switch {
	case n <= m.top:
		return n, true
	case n <= m.top+m.code:
		return n - m.top, false
	}
	return n - m.top - m.code, true
}

// lineShape is one way a compiler names a line: the pattern, the group
// holding the number, whether the next output line is an echo of the
// source, and the text that replaces a match inside locked code.
type lineShape struct {
	re       *regexp.Regexp
	num      int
	echoNext bool
	locked   func(n int) string
}

func lockedText(n int) string { return fmt.Sprintf("locked code, line %d", n) }

var (
	// g++: "main.cpp:12:5: error", also after "In file included from" or
	// "from" on an include chain. The column goes with a locked line.
	gppShape = lineShape{re: regexp.MustCompile(`^(\s*(?:In file included from |from )?)((?:[\w./-]*/)?main\.cpp):(\d{1,9})((?::\d{1,9})?)`),
		num: 3, locked: lockedText}
	// g++'s source echo: "   12 |     return 0;".
	gppGutter  = regexp.MustCompile(`^(\s*)(\d{1,9})( \| )`)
	javacShape = lineShape{re: regexp.MustCompile(`^((?:[\w./-]*/)?Main\.java):(\d{1,9})`), num: 2, echoNext: true, locked: lockedText}
	// Python: a frame, anywhere on the line (py_compile prints it after
	// "PyCompileError: "), and the bare form at the end of a message.
	pythonShape = lineShape{re: regexp.MustCompile(`File "((?:[^"\n]*/)?main\.py)", line (\d{1,9})`), num: 2, echoNext: true, locked: lockedText}
	pythonBare  = lineShape{re: regexp.MustCompile(`\(main\.py, line (\d{1,9})\)`), num: 1,
		locked: func(n int) string { return "(" + lockedText(n) + ")" }}
	nodeShape = lineShape{re: regexp.MustCompile(`^((?:[\w./-]*/)?main\.js):(\d{1,9})((?::\d{1,9})?)`), num: 2, echoNext: true, locked: lockedText}

	shapesByLanguage = map[string][]lineShape{
		"cpp17":   {gppShape},
		"java21":  {javacShape},
		"python3": {pythonShape, pythonBare},
		"node22":  {nodeShape},
	}
)

// rewrite maps every location in compiler output from the composed file to
// the student's code. Output of a file that was not composed is returned
// as it is.
func (m lineMap) rewrite(out, lang string) string {
	shapes := shapesByLanguage[lang]
	if m.top == 0 || out == "" || len(shapes) == 0 {
		return out
	}
	lines := strings.Split(out, "\n")
	echo := false
	for i, line := range lines {
		if echo {
			echo = false
			continue
		}
		if lang == "cpp17" {
			if g := gppGutter.FindStringSubmatchIndex(line); g != nil {
				lines[i] = m.gutter(line, g)
				continue
			}
		}
		for _, sh := range shapes {
			if loc := sh.re.FindStringSubmatchIndex(line); loc != nil {
				lines[i] = m.replace(line, loc, sh)
				echo = sh.echoNext
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

// replace rewrites the one location a shape found in line.
func (m lineMap) replace(line string, loc []int, sh lineShape) string {
	ns, ne := loc[2*sh.num], loc[2*sh.num+1]
	n, err := strconv.Atoi(line[ns:ne])
	if err != nil {
		return line
	}
	at, locked := m.place(n)
	if !locked {
		return line[:ns] + strconv.Itoa(at) + line[ne:]
	}
	// Keep any prefix the pattern matched before the file name (g++'s
	// include chain), and replace the file, line and column.
	start := loc[0]
	if sh.re == gppShape.re {
		start = loc[3]
	}
	return line[:start] + sh.locked(at) + line[loc[1]:]
}

// gutter renumbers a g++ echo line in the student's code, keeping its
// width so the caret line below still lines up. A locked line keeps the
// composed number: its text is the locked code itself.
func (m lineMap) gutter(line string, g []int) string {
	n, err := strconv.Atoi(line[g[4]:g[5]])
	if err != nil {
		return line
	}
	at, locked := m.place(n)
	if locked {
		return line
	}
	width := g[5] - g[2]
	return fmt.Sprintf("%*d", width, at) + line[g[5]:]
}

// rewriteRunStderr maps a run's stderr. Only Node's is rewritten: it has no
// compile step, so its syntax errors arrive here in the compiler shape.
// Runtime stack traces are not remapped (PLAN 3.3): of a JavaScript error
// only the first line, which names the place, changes.
func (m lineMap) rewriteRunStderr(stderr, lang string) string {
	if lang != "node22" {
		return stderr
	}
	return m.rewrite(stderr, lang)
}
