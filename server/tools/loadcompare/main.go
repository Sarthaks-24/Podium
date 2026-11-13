// Command loadcompare reads the runs of the replay corpus and prints pass or
// fail for one conditional speed change of v2.1.0 (J0, J1, J5 or J6)
// against thresholds written into compare.go before any of them was built.
//
// A run is a directory holding:
//
//	verdicts.jsonl  one Record per request, written by the replay driver
//	judge.log       the judge's log, JSON lines (journalctl -u podium-judge -o cat)
//	sampler.log     optional; JSON lines with a cumulative slice_oom_kills
//
// Usage:
//
//	loadcompare -change j5 -base A1,A2,A3 -test B1,B2,B3 [-aa N1,N2]
//
// -aa is the A/A pair that measures the noise. The exit status is 0 when the
// change ships and 1 when any check fails.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	change := flag.String("change", "", "j0, j1, j5 or j6")
	base := flag.String("base", "", "run directories of the arm with the change off, comma separated")
	test := flag.String("test", "", "run directories of the arm with the change on")
	aa := flag.String("aa", "", "run directories of the A/A pair; without them no noise is subtracted")
	flag.Parse()
	if *change == "" || *base == "" || *test == "" {
		flag.Usage()
		os.Exit(2)
	}
	b, err1 := loadRuns(*base)
	t, err2 := loadRuns(*test)
	n, err3 := loadRuns(*aa)
	for _, err := range []error{err1, err2, err3} {
		if err != nil {
			fmt.Fprintln(os.Stderr, "loadcompare:", err)
			os.Exit(2)
		}
	}
	if *aa == "" {
		fmt.Println("note: no A/A pair given, so noise is taken as zero and only the floors apply")
	}
	rs, err := Change(*change, b, t, n)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadcompare:", err)
		os.Exit(2)
	}
	printResults(os.Stdout, strings.ToUpper(*change), rs)
	if !Ships(rs) {
		os.Exit(1)
	}
}

func printResults(w io.Writer, change string, rs []Result) {
	for _, r := range rs {
		tag := "FAIL"
		switch {
		case r.Pass:
			tag = "PASS"
		case r.Skip:
			tag = "SKIP"
		}
		fmt.Fprintf(w, "%s  %s: %s\n", tag, r.Name, r.Detail)
	}
	if Ships(rs) {
		fmt.Fprintf(w, "RESULT %s: ships\n", change)
	} else {
		fmt.Fprintf(w, "RESULT %s: does not ship\n", change)
	}
}

func loadRuns(list string) ([]*Run, error) {
	var runs []*Run
	for _, d := range strings.Split(list, ",") {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		r, err := loadRun(d)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}

// loadRun reads one run directory. A missing verdicts or judge file is an
// error; a missing sampler file is not.
func loadRun(dir string) (*Run, error) {
	r := &Run{Name: filepath.Base(dir), Timing: map[string]Timing{}}
	err := eachLine(filepath.Join(dir, "verdicts.jsonl"), func(b []byte) {
		var rec Record
		if json.Unmarshal(b, &rec) == nil {
			r.Records = append(r.Records, rec)
		}
	})
	if err != nil {
		return nil, err
	}
	if len(r.Records) == 0 {
		return nil, fmt.Errorf("%s: no records in verdicts.jsonl", dir)
	}
	err = eachLine(filepath.Join(dir, "judge.log"), func(b []byte) {
		var line struct {
			Msg string `json:"msg"`
			Timing
		}
		if json.Unmarshal(b, &line) == nil && line.Msg == "job timing" {
			r.Timing[timingKey(line.Kind, line.SubjectID)] = line.Timing
		}
	})
	if err != nil {
		return nil, err
	}
	var first, last int64 = -1, 0
	if err := eachLine(filepath.Join(dir, "sampler.log"), func(b []byte) {
		var s struct {
			Kills *int64 `json:"slice_oom_kills"`
		}
		if json.Unmarshal(b, &s) == nil && s.Kills != nil {
			if first < 0 {
				first = *s.Kills
			}
			last = *s.Kills
			r.HasSampler = true
		}
	}); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if r.HasSampler {
		r.OOMKills = last - first
	}
	return r, nil
}

// eachLine calls f for each line of a file. A JSON line may follow a
// prefix such as a journal timestamp; it starts at the first brace.
func eachLine(path string, f func([]byte)) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		b := sc.Bytes()
		if i := strings.IndexByte(string(b), '{'); i > 0 {
			b = b[i:]
		}
		f(b)
	}
	return sc.Err()
}
