package problems

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// age sets every blob row's created_at to t.
func age(t *testing.T, s *Service, at time.Time) {
	t.Helper()
	if _, err := s.DB.Exec("UPDATE blobs SET created_at = ?", at.UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func put(t *testing.T, s *Service, data string) string {
	t.Helper()
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	sha, err := s.Blobs.Put(ctx, tx, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return sha
}

func onDisk(s *Service, sha string) bool {
	_, err := os.Stat(s.Blobs.path(sha))
	return err == nil
}

func hasRow(t *testing.T, s *Service, sha string) bool {
	t.Helper()
	var n int
	if err := s.DB.QueryRow("SELECT count(*) FROM blobs WHERE sha = ?", sha).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// writeOrphan writes a file with no row, modified at mtime.
func writeOrphan(t *testing.T, s *Service, data string, mtime time.Time) string {
	t.Helper()
	sha, err := s.Blobs.Write([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	os.Chtimes(s.Blobs.path(sha), mtime, mtime)
	return sha
}

func TestGCKeepsEverythingReferenced(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	now := time.Now()
	old := now.Add(-48 * time.Hour)

	// A queued validation pins its programs; a submission pins its source.
	if _, err := s.RequestValidation(ctx, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	submitted := put(t, s, "int main() { return 0; } // a student's program")
	if _, err := s.DB.Exec(`INSERT INTO users (email, created_at) VALUES ('asha@college.edu', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at)
		VALUES (?, 1, 'submit', 'cpp17', 'x', ?, 1)`, p.ID, submitted); err != nil {
		t.Fatal(err)
	}
	unused := put(t, s, "an input from a run long ago")
	age(t, s, old)
	fresh := put(t, s, "a custom input from a minute ago")
	oldOrphan := writeOrphan(t, s, "an upload nobody recorded", old)
	newOrphan := writeOrphan(t, s, "an upload in progress", now)

	res, err := s.Blobs.GC(ctx, s.DB, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows != 1 || res.Files != 1 || res.Bytes != int64(len("an input from a run long ago")+len("an upload nobody recorded")) {
		t.Errorf("result %+v, want one row and one orphan", res)
	}
	if hasRow(t, s, unused) || onDisk(s, unused) {
		t.Error("an old blob nothing refers to was kept")
	}
	if onDisk(s, oldOrphan) {
		t.Error("an old file with no row was kept")
	}
	for name, sha := range map[string]string{"recent blob": fresh, "submission source": submitted} {
		if !hasRow(t, s, sha) || !onDisk(s, sha) {
			t.Errorf("%s removed", name)
		}
	}
	if !onDisk(s, newOrphan) {
		t.Error("a recent file with no row (an upload in progress) was removed")
	}
	got, _ := s.Get(ctx, p.ID)
	for _, tc := range got.Tests {
		if !onDisk(s, tc.InputSHA) || !onDisk(s, tc.OutputSHA) {
			t.Fatalf("test %d lost a file", tc.Idx)
		}
	}
	r, err := s.Blobs.Verify(ctx, s.DB, true)
	if err != nil || !r.OK() {
		t.Errorf("verify after gc: %+v %v", r, err)
	}

	// The validation's programs stay while the job waits, and for a week
	// after it closes; then only the tests remain.
	pl := fakeJudge(t, s, 3, nil)
	refSHA := pl.Programs.Reference.SourceSHA
	if res, _ := s.Blobs.GC(ctx, s.DB, now); res.Rows != 0 || !onDisk(s, refSHA) {
		t.Fatalf("a closed job's program removed within the week: %+v", res)
	}
	if _, err := s.DB.Exec("UPDATE jobs SET created_at = ?", now.Add(-8*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.Blobs.GC(ctx, s.DB, now); res.Rows == 0 || onDisk(s, refSHA) || hasRow(t, s, refSHA) {
		t.Errorf("a week-old job's program kept: %+v", res)
	}
	// Validating again stores the program again.
	if _, err := s.RequestValidation(ctx, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	if !onDisk(s, refSHA) || !hasRow(t, s, refSHA) {
		t.Error("the reference was not stored again for the new validation")
	}
}

func TestGCSkipsBlobsRecordedAgain(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	now := time.Now()
	sha := put(t, s, "stored long ago, used again today")
	age(t, s, now.Add(-72*time.Hour))
	put(t, s, "stored long ago, used again today")
	if res, err := s.Blobs.GC(ctx, s.DB, now); err != nil || res.Rows != 0 || !onDisk(s, sha) {
		t.Errorf("a blob recorded again was removed: %+v %v", res, err)
	}
}

func TestRecordNoticesARemovedFile(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	data := []byte("removed by gc between write and record")
	sha, err := s.Blobs.Write(data)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(s.Blobs.path(sha))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := s.Blobs.Record(ctx, tx, sha, int64(len(data))); !errors.Is(err, ErrBlobMissing) {
		t.Errorf("record of a missing file: %v, want ErrBlobMissing", err)
	}
}

func TestGCKeepsFilesNamedByOpenJobs(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	now := time.Now()
	// An output a generate job's worker uploaded, not recorded yet, an
	// old file: kept while its job is open.
	upload := writeOrphan(t, s, "an output under a long lease", now.Add(-48*time.Hour))
	if _, err := s.DB.Exec(`INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('validate', 10, 'leased', ?, ?)`,
		`{"tests":[{"input_sha":"`+upload+`"}]}`, now.Add(-30*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Blobs.GC(ctx, s.DB, now); err != nil || res.Files != 0 || !onDisk(s, upload) {
		t.Errorf("a file an open job names was removed: %+v %v", res, err)
	}
}

func TestVerifyFindsDamage(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	r, err := s.Blobs.Verify(ctx, s.DB, true)
	if err != nil || !r.OK() || r.Blobs == 0 || r.Orphans != 0 {
		t.Fatalf("clean bank: %+v %v", r, err)
	}
	got, _ := s.Get(ctx, p.ID)
	os.Remove(s.Blobs.path(got.Tests[0].InputSHA))
	os.WriteFile(s.Blobs.path(got.Tests[1].InputSHA), []byte("changed!!"), 0o600)
	os.WriteFile(s.Blobs.path(got.Tests[2].OutputSHA), make([]byte, got.Tests[2].OutputSize), 0o600)
	writeOrphan(t, s, "stray", time.Now())
	r, err = s.Blobs.Verify(ctx, s.DB, true)
	if err != nil || r.OK() || len(r.Missing) != 1 || len(r.WrongSize) != 1 || len(r.BadHash) != 1 || r.Orphans != 1 {
		t.Errorf("damaged bank: missing %d, wrong size %d, bad hash %d, orphans %d, %v",
			len(r.Missing), len(r.WrongSize), len(r.BadHash), r.Orphans, err)
	}
	// Tests reference blobs through a foreign key; submissions do not.
	lost := "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := s.DB.Exec(`INSERT INTO users (email, created_at) VALUES ('asha@college.edu', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at)
		VALUES (?, 1, 'submit', 'cpp17', 'x', ?, 1)`, p.ID, lost); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Blobs.Verify(ctx, s.DB, false)
	if len(r.Dangling) != 1 || r.Dangling[0] != lost {
		t.Errorf("dangling %v, want the submission's lost source", r.Dangling)
	}
}

func TestHexRuns(t *testing.T) {
	a := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	set := map[string]bool{}
	hexRuns(`{"x":"`+a+`","short":"abc","long":"`+a+`0","upper":"`+a[:60]+`ABCD"}`, set)
	if len(set) != 1 || !set[a] {
		t.Errorf("runs %v, want only the 64-digit one", set)
	}
}
