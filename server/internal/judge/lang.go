package judge

import (
	"strconv"
	"strings"
	"time"
)

// Language describes how to compile and run one supported language inside
// isolate. The limits are calibrated on the judge host (podium-judge
// calibrate).
type Language struct {
	// ID is the stable identifier stored with submissions.
	ID string
	// Name is shown to students.
	Name string
	// Source is the file name the submission is written to.
	Source string
	// Compile is the compile command; nil means the language is interpreted
	// and only checked at run time.
	Compile []string
	// CompileProcesses and CompileMemoryMB bound the compiler.
	CompileProcesses int
	CompileMemoryMB  int
	// CompileCPU overrides the default 10 s compile time limit; zero keeps
	// the default. Only the checker language sets it.
	CompileCPU time.Duration
	// Run is the run command. "{mem}" is replaced with the problem memory
	// limit in megabytes.
	Run []string
	// TimeFactor multiplies the problem time limit.
	TimeFactor float64
	// MemoryOverheadMB is added to the problem memory limit for the control
	// group, to cover the runtime itself (JVM, V8, CPython).
	MemoryOverheadMB int
	// Processes caps processes plus threads at run time.
	Processes int
	// Exclusive marks compiles and runs that take the judge-wide mutex
	// (g++ and every JVM invocation), keeping peak RAM inside the budget.
	CompileExclusive bool
	RunExclusive     bool
	// Dirs are extra isolate --dir rules the toolchain needs.
	Dirs []string
	// OOMMarkers are stderr fragments that mean the runtime itself ran out
	// of its heap, reported as memory limit exceeded rather than a crash.
	OOMMarkers []string
}

// javaHome is the Ubuntu 24.04 path of OpenJDK 21. /usr/bin/java is a
// symlink through /etc/alternatives, which is not visible inside the box,
// so the JDK is invoked by its real path.
const javaHome = "/usr/lib/jvm/java-21-openjdk-amd64"

// Languages is the default language table, keyed by ID.
var Languages = map[string]Language{
	"cpp17": {
		ID: "cpp17", Name: "C++17", Source: "main.cpp",
		Compile:          []string{"/usr/bin/g++", "-O2", "-std=gnu++17", "-pipe", "-static", "-o", "main", "main.cpp"},
		CompileProcesses: 16, CompileMemoryMB: 512, CompileExclusive: true,
		Run:        []string{"./main"},
		TimeFactor: 1.0, MemoryOverheadMB: 0, Processes: 1,
	},
	"java21": {
		ID: "java21", Name: "Java 21", Source: "Main.java",
		Compile: []string{javaHome + "/bin/javac", "-J-Xmx256m", "-J-XX:+UseSerialGC", "-J-XX:TieredStopAtLevel=1",
			"-encoding", "UTF-8", "Main.java"},
		CompileProcesses: 64, CompileMemoryMB: 512, CompileExclusive: true,
		Run: []string{javaHome + "/bin/java", "-Xmx{mem}m", "-Xss64m", "-XX:+UseSerialGC",
			"-XX:TieredStopAtLevel=1", "-XX:-UsePerfData", "-Xshare:auto", "Main"},
		TimeFactor: 2.0, MemoryOverheadMB: 256, Processes: 64, RunExclusive: true,
		Dirs:       []string{"/etc/java-21-openjdk:maybe"},
		OOMMarkers: []string{"java.lang.OutOfMemoryError"},
	},
	"python3": {
		ID: "python3", Name: "Python 3", Source: "main.py",
		Compile:          []string{"/usr/bin/python3", "-c", "import py_compile; py_compile.compile('main.py', doraise=True)"},
		CompileProcesses: 1, CompileMemoryMB: 128,
		Run:        []string{"/usr/bin/python3", "main.py"},
		TimeFactor: 3.0, MemoryOverheadMB: 64, Processes: 1,
		OOMMarkers: []string{"MemoryError"},
	},
	"node22": {
		ID: "node22", Name: "JavaScript (Node 22)", Source: "main.js",
		Run:        []string{"/usr/bin/node", "--stack-size=65500", "--max-old-space-size={mem}", "main.js"},
		TimeFactor: 2.0, MemoryOverheadMB: 128, Processes: 16,
		OOMMarkers: []string{"JavaScript heap out of memory"},
	},
}

// CheckerLanguage is C++17 with a longer compile budget: testlib.h is a
// 6,000-line header and a checker takes several seconds to build, which a
// student submission never needs. Checkers are setter code and are still
// compiled and run inside isolate.
func CheckerLanguage() Language {
	l := Languages["cpp17"]
	l.CompileCPU = 30 * time.Second
	return l
}

// RunCommand returns the run command with the memory limit substituted.
func (l Language) RunCommand(memoryMB int) []string {
	out := make([]string, len(l.Run))
	for i, a := range l.Run {
		out[i] = strings.ReplaceAll(a, "{mem}", strconv.Itoa(memoryMB))
	}
	return out
}

// sandboxEnv is the complete environment of every sandboxed process.
var sandboxEnv = []string{
	"PATH=/usr/bin:/bin",
	"HOME=/box",
	"TMPDIR=/tmp",
	"LANG=C.UTF-8",
}
