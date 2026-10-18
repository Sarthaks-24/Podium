package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// DefaultCompileCacheBytes caps the compile cache (PLAN.md 4.1 lever 3).
const DefaultCompileCacheBytes = 500 << 20

// CompileCache keeps compiled programs under Dir/<key>/, keyed by language,
// compile command and source. "Run on samples" followed by "Submit" of the
// same code, identical resubmissions, and a problem's checker compile once.
// Compile errors are cached too, so the same broken code is not rebuilt.
// A compile that ran out of time or memory is cached for transientTTL
// only. Entries in use by a running job are never evicted.
type CompileCache struct {
	Dir      string
	MaxBytes int64

	mu    sync.Mutex
	inUse map[string]int
	now   func() time.Time // tests replace it
}

// transientTTL bounds how long a compile that ran out of time or memory is
// cached: long enough that a student resending the same code does not hold
// the judge-wide compile lock again, short enough that a failure caused by
// load stops being reported once the load has passed.
const transientTTL = 10 * time.Minute

type cachedResult struct {
	Verdict   judge.Verdict `json:"verdict"`
	Output    string        `json:"output"`
	Transient bool          `json:"transient,omitempty"`
	// Expires is when a transient entry stops counting, in Unix ms.
	Expires int64 `json:"expires,omitempty"`
}

func (c *CompileCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func compileKey(lang judge.Language, src []byte) string {
	h := sha256.New()
	h.Write([]byte(lang.ID + "\x00" + strings.Join(lang.Compile, "\x01") + "\x00" + lang.CompileCPU.String() + "\x00"))
	h.Write(src)
	return hex.EncodeToString(h.Sum(nil))
}

func (c *CompileCache) acquire(key string) func() {
	c.mu.Lock()
	if c.inUse == nil {
		c.inUse = map[string]int{}
	}
	c.inUse[key]++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.inUse[key]--
			if c.inUse[key] <= 0 {
				delete(c.inUse, key)
			}
			c.mu.Unlock()
		})
	}
}

// Compile returns the compiled program for src, building it in box on a
// miss. Call release when the job no longer needs the program.
func (c *CompileCache) Compile(ctx context.Context, r *judge.Runner, box int, lang judge.Language, src []byte) (judge.Program, judge.CompileResult, func(), error) {
	key := compileKey(lang, src)
	dir := filepath.Join(c.Dir, key)
	release := c.acquire(key)
	if res, ok := c.load(dir); ok {
		return judge.Program{Lang: lang, Dir: filepath.Join(dir, "bin")}, res, release, nil
	}
	fail := func(err error) (judge.Program, judge.CompileResult, func(), error) {
		release()
		return judge.Program{}, judge.CompileResult{}, func() {}, err
	}
	tmp, err := os.MkdirTemp(c.Dir, ".build-*")
	if err != nil {
		return fail(err)
	}
	bin := filepath.Join(tmp, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		os.RemoveAll(tmp)
		return fail(err)
	}
	_, res, err := r.Compile(ctx, box, lang, src, bin)
	if err != nil {
		os.RemoveAll(tmp)
		return fail(err)
	}
	moved, err := c.store(tmp, dir, res)
	if err != nil {
		os.RemoveAll(tmp)
		return fail(err)
	}
	if !moved {
		// Another lane stored the same key meanwhile (possibly with a
		// different outcome, if one compile ran out of time under load).
		// This job keeps its own build, removed on release.
		return judge.Program{Lang: lang, Dir: bin}, res, func() { release(); os.RemoveAll(tmp) }, nil
	}
	return judge.Program{Lang: lang, Dir: filepath.Join(dir, "bin")}, res, release, nil
}

// store writes the result next to the build in tmp and moves it to dir.
// moved is false, and tmp left in place, when dir already exists.
func (c *CompileCache) store(tmp, dir string, res judge.CompileResult) (moved bool, err error) {
	r := cachedResult{Verdict: res.Verdict, Output: res.Output, Transient: res.Transient}
	if res.Transient {
		r.Expires = c.clock().Add(transientTTL).UnixMilli()
	}
	meta, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(tmp, "result.json"), meta, 0o600); err != nil {
		return false, err
	}
	return os.Rename(tmp, dir) == nil, nil
}

func (c *CompileCache) load(dir string) (judge.CompileResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		return judge.CompileResult{}, false
	}
	var r cachedResult
	if json.Unmarshal(b, &r) != nil {
		return judge.CompileResult{}, false
	}
	now := c.clock()
	if r.Transient && now.UnixMilli() >= r.Expires {
		// Expired: moved aside in one step, so a lane building the same
		// key never finds it half deleted. It holds no program to run.
		trash, err := os.MkdirTemp(c.Dir, ".evict-*")
		if err == nil {
			if os.Rename(dir, filepath.Join(trash, "entry")) != nil {
				os.RemoveAll(dir)
			}
			os.RemoveAll(trash)
		}
		return judge.CompileResult{}, false
	}
	os.Chtimes(dir, now, now) // mark as recently used
	return judge.CompileResult{Verdict: r.Verdict, Output: r.Output, Transient: r.Transient}, true
}

// Evict removes least recently used entries until the cache is under its
// cap, never touching an entry a running job holds. The worker calls it
// after every job.
func (c *CompileCache) Evict() {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := c.MaxBytes
	if limit <= 0 {
		limit = DefaultCompileCacheBytes
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return
	}
	type entry struct {
		key  string
		used time.Time
		size int64
	}
	var all []entry
	var total int64
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		var size int64
		filepath.WalkDir(filepath.Join(c.Dir, e.Name()), func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if fi, err := d.Info(); err == nil {
					size += fi.Size()
				}
			}
			return nil
		})
		all = append(all, entry{e.Name(), info.ModTime(), size})
		total += size
	}
	sort.Slice(all, func(i, j int) bool { return all[i].used.Before(all[j].used) })
	for _, e := range all {
		if total <= limit {
			return
		}
		if c.inUse[e.key] > 0 {
			continue
		}
		// Renamed aside first, so a concurrent load sees it gone rather
		// than half deleted.
		p := filepath.Join(c.Dir, e.key)
		trash := filepath.Join(c.Dir, ".evict-"+e.key)
		if os.Rename(p, trash) == nil {
			os.RemoveAll(trash)
			total -= e.size
		}
	}
}
