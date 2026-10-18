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
// Compile errors are cached too, so the same broken code is not rebuilt,
// but failures caused by load (the compiler timing out or running out of
// memory) are not. Entries in use by a running job are never evicted.
type CompileCache struct {
	Dir      string
	MaxBytes int64

	mu    sync.Mutex
	inUse map[string]int
}

type cachedResult struct {
	Verdict judge.Verdict `json:"verdict"`
	Output  string        `json:"output"`
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
	if res.Transient {
		// Not cached: under less load the same code may compile. The
		// program dir is the temporary one, removed on release.
		return judge.Program{Lang: lang, Dir: bin}, res, func() { release(); os.RemoveAll(tmp) }, nil
	}
	meta, _ := json.Marshal(cachedResult{Verdict: res.Verdict, Output: res.Output})
	if err := os.WriteFile(filepath.Join(tmp, "result.json"), meta, 0o600); err != nil {
		os.RemoveAll(tmp)
		return fail(err)
	}
	// Another lane may have built the same key meanwhile; either copy is
	// good, and the loser's is discarded.
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
	}
	return judge.Program{Lang: lang, Dir: filepath.Join(dir, "bin")}, res, release, nil
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
	now := time.Now()
	os.Chtimes(dir, now, now) // mark as recently used
	return judge.CompileResult{Verdict: r.Verdict, Output: r.Output}, true
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
