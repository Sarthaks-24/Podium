// Command loadreplay sends the fixed replay corpus (tools/loadtest/corpus)
// to a podium server in testing mode, waits for every verdict and writes
// verdicts.jsonl for cmd/loadcompare. It needs no k6. The programs use the
// packs in problems/, which tools/loadtest/seed.py imports once per stack.
//
//	loadreplay -base http://127.0.0.1:18080 -rounds 2 -minutes 3 -burst 1 -out run1
//
// The exit status is 1 when any verdict differs from the one recorded beside
// the program in corpus.json, so the same command is the local check.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	var (
		base     = flag.String("base", "http://127.0.0.1:8080", "the server")
		origin   = flag.String("origin", "http://localhost:5173", "the server's PODIUM_BASE_URL, sent as Origin")
		root     = flag.String("root", ".", "the repository root; corpus files are relative to it")
		manifest = flag.String("corpus", "tools/loadtest/corpus/corpus.json", "the corpus manifest, relative to -root")
		rounds   = flag.Int("rounds", 1, "how many rounds of the corpus to send (one round is 51 requests)")
		minutes  = flag.Float64("minutes", 5, "length of the replay")
		burst    = flag.Float64("burst", 5, "the last this many minutes send 3 times as densely")
		tail     = flag.Duration("tail", 30*time.Second, "the contest closes this long before the replay ends, so the last requests are practice")
		students = flag.Int("students", 60, "signed-in students the requests are dealt to")
		timeout  = flag.Duration("timeout", 15*time.Minute, "give up on one request after this long")
		out      = flag.String("out", "", "directory for verdicts.jsonl")
		list     = flag.Bool("list", false, "print the request list and stop; no server needed")
		only     = flag.String("only", "", "send only these program ids, comma separated (for checking one program; a comparison run sends them all)")
	)
	flag.Parse()
	c, err := LoadCorpus(*root, filepath.Join(*root, filepath.FromSlash(*manifest)))
	if err != nil {
		fatal(err)
	}
	if *only != "" {
		if c, err = c.Only(strings.Split(*only, ",")); err != nil {
			fatal(err)
		}
	}
	items := c.Sequence(*rounds)
	total := time.Duration(*minutes * float64(time.Minute))
	offsets, firstBurst := Schedule(len(items), total, time.Duration(*burst*float64(time.Minute)))
	if *list {
		for i, it := range items {
			fmt.Printf("%4d %7.1fs %-7s %-17s%s\n", it.Seq, offsets[i].Seconds(), it.Kind, it.Program.ID, map[bool]string{true: " burst"}[i >= firstBurst])
		}
		return
	}
	if *out == "" {
		fatal(fmt.Errorf("-out is required"))
	}
	if *tail >= total {
		fatal(fmt.Errorf("-tail must be shorter than the replay"))
	}
	if err := checkOutDir(*out); err != nil {
		fatal(err)
	}
	d := &Driver{Base: strings.TrimRight(*base, "/"), Origin: *origin, Students: *students, Poll: 250 * time.Millisecond, Timeout: *timeout, Tail: *tail}
	recs, err := d.Replay(context.Background(), c, items, offsets, firstBurst, total)
	if err != nil {
		fatal(err)
	}
	if err := writeRecords(*out, recs); err != nil {
		fatal(err)
	}
	if bad := summarize(os.Stdout, recs); bad > 0 {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "loadreplay:", err)
	os.Exit(2)
}

func writeRecords(dir string, recs []Record) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "verdicts.jsonl"))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

// summarize prints, for each program and kind, the verdicts seen and the
// slowest test time, and returns how many requests had a verdict other than
// the expected one.
func summarize(w io.Writer, recs []Record) (bad int) {
	type key struct{ program, kind string }
	type agg struct {
		expected string
		got      map[string]int
		maxMS    int64
	}
	by := map[key]*agg{}
	var keys []key
	for _, r := range recs {
		k := key{r.Program, r.Kind}
		a := by[k]
		if a == nil {
			a = &agg{expected: r.Expected, got: map[string]int{}}
			by[k] = a
			keys = append(keys, k)
		}
		v := r.Verdict
		if v == "" {
			v = "none (" + r.Detail + ")"
		}
		a.got[v]++
		a.maxMS = max(a.maxMS, r.TimeMS)
		if r.Verdict != r.Expected {
			bad++
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].program != keys[j].program {
			return keys[i].program < keys[j].program
		}
		return keys[i].kind < keys[j].kind
	})
	for _, k := range keys {
		a := by[k]
		var seen []string
		for v, n := range a.got {
			seen = append(seen, fmt.Sprintf("%s x%d", v, n))
		}
		sort.Strings(seen)
		mark := "ok  "
		if len(a.got) != 1 || a.got[a.expected] == 0 {
			mark = "BAD "
		}
		fmt.Fprintf(w, "%s %-17s %-6s expected %-4s got %s, slowest test %d ms\n", mark, k.program, k.kind, a.expected, strings.Join(seen, ", "), a.maxMS)
	}
	fmt.Fprintf(w, "%d of %d requests had an unexpected verdict\n", bad, len(recs))
	return bad
}

// checkOutDir refuses an output directory that already holds files: writing
// there would overwrite an earlier run. A directory that does not exist yet
// is fine.
func checkOutDir(dir string) error {
	fi, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("-out %s is not a directory", dir)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(ents) > 0 {
		return fmt.Errorf("-out %s already holds files; use a new directory", dir)
	}
	return nil
}
