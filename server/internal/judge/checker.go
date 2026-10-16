package judge

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

// CheckerType selects how output is compared (PLAN.md section 7.4).
type CheckerType string

const (
	CheckTokens CheckerType = "tokens"
	CheckExact  CheckerType = "exact"
	CheckFloat  CheckerType = "float"
	// CheckCustom (a testlib checker run in the sandbox) arrives in M2.
	CheckCustom CheckerType = "custom"
)

// DefaultEpsilon is the float checker's default absolute and relative error.
const DefaultEpsilon = 1e-6

// maxCompareBytes bounds how much of either stream a checker reads. Output
// is already capped at 16 MB by --fsize; this guards the expected side too.
const maxCompareBytes = 64 << 20

// maxToken is the longest single token the token checkers accept. A longer
// token in the student's output is a wrong answer; two lanes comparing at
// once then hold at most 4 x 4 MB of scanner buffers.
const maxToken = 4 << 20

// Result is a checker's judgement. Message is safe to show to the student
// under the "full" feedback mode and never contains more than a short
// excerpt of either output.
type Result struct {
	OK      bool
	Message string
}

// Check compares actual output with the expected answer.
func Check(kind CheckerType, expected, actual io.Reader, epsilon float64) (Result, error) {
	expected = io.LimitReader(expected, maxCompareBytes)
	actual = io.LimitReader(actual, maxCompareBytes)
	switch kind {
	case CheckTokens, "":
		return compareTokens(expected, actual, nil)
	case CheckFloat:
		if epsilon <= 0 {
			epsilon = DefaultEpsilon
		}
		return compareTokens(expected, actual, func(e, a []byte) bool { return floatEqual(e, a, epsilon) })
	case CheckExact:
		return compareExact(expected, actual)
	}
	return Result{}, fmt.Errorf("checker %q is not supported here", kind)
}

func compareTokens(expected, actual io.Reader, eq func(e, a []byte) bool) (Result, error) {
	es, as := tokenScanner(expected), tokenScanner(actual)
	for n := 1; ; n++ {
		eok, aok := es.Scan(), as.Scan()
		if errors.Is(as.Err(), bufio.ErrTooLong) {
			return Result{Message: fmt.Sprintf("Token %d is longer than %d MB.", n, maxToken>>20)}, nil
		}
		if err := firstErr(es.Err(), as.Err()); err != nil {
			return Result{}, err
		}
		switch {
		case !eok && !aok:
			return Result{OK: true}, nil
		case !aok:
			return Result{Message: fmt.Sprintf("Output ended early: expected %s at token %d.", excerpt(es.Bytes()), n)}, nil
		case !eok:
			return Result{Message: fmt.Sprintf("Extra output from token %d: found %s.", n, excerpt(as.Bytes()))}, nil
		}
		e, a := es.Bytes(), as.Bytes()
		if bytes.Equal(e, a) || (eq != nil && eq(e, a)) {
			continue
		}
		return Result{Message: fmt.Sprintf("Token %d differs: expected %s, found %s.", n, excerpt(e), excerpt(a))}, nil
	}
}

func tokenScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64<<10), maxToken)
	s.Split(bufio.ScanWords)
	return s
}

func floatEqual(e, a []byte, eps float64) bool {
	ev, err1 := strconv.ParseFloat(string(e), 64)
	av, err2 := strconv.ParseFloat(string(a), 64)
	if err1 != nil || err2 != nil || math.IsNaN(av) || math.IsInf(av, 0) {
		return false
	}
	diff := math.Abs(ev - av)
	return diff <= eps || diff <= eps*math.Abs(ev)
}

// compareExact streams both outputs through canonicalReader, so neither is
// held in memory.
func compareExact(expected, actual io.Reader) (Result, error) {
	e, a := newCanonicalReader(expected), newCanonicalReader(actual)
	line := 1
	for {
		eb, eerr := e.next()
		ab, aerr := a.next()
		if err := firstErr(ignoreEOF(eerr), ignoreEOF(aerr)); err != nil {
			return Result{}, err
		}
		if eerr == io.EOF && aerr == io.EOF {
			return Result{OK: true}, nil
		}
		if eerr != nil || aerr != nil || eb != ab {
			return Result{Message: fmt.Sprintf("Output differs from line %d.", line)}, nil
		}
		if ab == '\n' {
			line++
		}
	}
}

// canonicalReader yields a byte stream with CRLF turned into LF and
// trailing newlines dropped, so an expected file saved on Windows still
// matches. Newlines are held back until a later byte shows they are not
// trailing.
type canonicalReader struct {
	r       *bufio.Reader
	held    byte
	hasHeld bool
	queued  int // newlines to emit before held
}

func newCanonicalReader(r io.Reader) *canonicalReader {
	return &canonicalReader{r: bufio.NewReaderSize(r, 64<<10)}
}

func (c *canonicalReader) next() (byte, error) {
	if c.queued > 0 {
		c.queued--
		return '\n', nil
	}
	if c.hasHeld {
		c.hasHeld = false
		return c.held, nil
	}
	newlines := 0
	for {
		b, err := c.readNormalised()
		if err != nil {
			return 0, err // pending newlines were trailing: dropped
		}
		if b == '\n' {
			newlines++
			continue
		}
		if newlines == 0 {
			return b, nil
		}
		c.queued, c.held, c.hasHeld = newlines-1, b, true
		return '\n', nil
	}
}

func (c *canonicalReader) readNormalised() (byte, error) {
	b, err := c.r.ReadByte()
	if err != nil || b != '\r' {
		return b, err
	}
	nb, err := c.r.ReadByte()
	if err == nil && nb == '\n' {
		return '\n', nil
	}
	if err == nil {
		c.r.UnreadByte()
	}
	return '\r', nil
}

func ignoreEOF(err error) error {
	if err == io.EOF {
		return nil
	}
	return err
}

func excerpt(b []byte) string {
	const max = 32
	if len(b) > max {
		return strconv.Quote(string(b[:max])) + "..."
	}
	return strconv.Quote(string(b))
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
