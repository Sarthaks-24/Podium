package problems

// AI edit (v2.0.2 stage E): the pack is the schema. The model sees the
// problem's exported pack files without test data, answers with the files it
// changes, and the answer is merged over the current files and read by the
// real pack parser. Accept saves the merge through replaceIn as one new
// revision. Nothing here talks to a model.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// ErrEditStale says the problem changed after the AI draft was made.
var ErrEditStale = errors.New("The problem changed after this draft was made. Ask for the edit again.")

// MaxEditFiles bounds a model's reply: the pack's own entry limit is far
// above anything a problem needs, so the reply gets a tighter one.
const MaxEditFiles = 40

// EditSnapshot is a problem's pack as the model sees it.
type EditSnapshot struct {
	ID int64
	// Hash fingerprints Files; Accept refuses a draft whose base is no
	// longer the current pack.
	Hash string
	// Slug is the slug a reply must keep.
	Slug string
	// Mode is ModeFunction or ModeWhole: a reply may not change it.
	Mode string
	// Files are the pack's files by name, without test data.
	Files map[string][]byte
}

// EditSnapshot exports problem id as pack files for the model: every file of
// the exported pack except tests/. A deleted problem answers ErrDeleted and
// an archived one asks to be restored first.
func (s *Service) EditSnapshot(ctx context.Context, id int64) (*EditSnapshot, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return snapshotOf(p)
}

// snapshotOf builds the snapshot of a loaded problem.
func snapshotOf(p *Problem) (*EditSnapshot, error) {
	if p.Deleted {
		return nil, ErrDeleted
	}
	if p.Archived {
		return nil, FieldErrors{"problem": "Restore the problem before editing it with AI."}
	}
	var buf bytes.Buffer
	// Test data is never read: the model does not get it.
	if err := WritePack(&buf, p, func(string) ([]byte, error) { return nil, nil }); err != nil {
		return nil, err
	}
	files, err := readZip(buf.Bytes())
	if err != nil {
		return nil, err
	}
	for name := range files {
		if testFile.MatchString(name) {
			delete(files, name)
		}
	}
	mode := ModeWhole
	if p.IsFunction() {
		mode = ModeFunction
	}
	return &EditSnapshot{ID: p.ID, Hash: stateHash(p, files), Slug: p.Slug, Mode: mode, Files: files}, nil
}

// stateHash fingerprints the text files and the tests (by their SHAs), so a
// draft goes stale when either changes.
func stateHash(p *Problem, files map[string][]byte) string {
	h := sha256.New()
	h.Write([]byte(filesHash(files)))
	for _, t := range p.Tests {
		fmt.Fprintf(h, "|%d:%s:%s", t.Idx, t.InputSHA, t.OutputSHA)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// filesHash fingerprints the text files. The content hash is not used
// because it leaves out the statement, which an AI edit can change.
func filesHash(files map[string][]byte) string {
	h := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fmt.Fprintf(h, "%d:%s:%d:", len(name), name, len(files[name]))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// editFileName reports whether name is a file a reply may carry: the pack's
// text files, never test data.
func editFileName(name string) (ok bool, problem string) {
	switch {
	case testFile.MatchString(name) || strings.HasPrefix(name, "tests/"):
		return false, fmt.Sprintf("%s: AI edit cannot change test data. Edit the problem's tests in the editor.", name)
	case name == "problem.yaml", name == "statement.md", name == packPlanFile, name == packCheckerSource, sourceFile.MatchString(name):
		return true, ""
	}
	return false, fmt.Sprintf("%s is not a pack file. A pack holds problem.yaml, statement.md, tests.yaml, checker.cpp and source files at its top level.", name)
}

// acquirePackSlot waits for the pack slot that serialises the heavy pack
// operations, or gives up when ctx ends.
func acquirePackSlot(ctx context.Context) (func(), error) {
	select {
	case packOps <- struct{}{}:
		return func() { <-packOps }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// testFiles reads the problem's test data as pack files.
func (s *Service) testFiles(p *Problem) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, t := range p.Tests {
		for _, part := range []struct{ ext, sha string }{{"in", t.InputSHA}, {"out", t.OutputSHA}} {
			if part.sha == "" {
				continue
			}
			body, err := s.Blobs.Read(part.sha, MaxPackFile+1)
			if err != nil {
				return nil, fmt.Errorf("test %d %s: %w", t.Idx, part.ext, err)
			}
			out[fmt.Sprintf("tests/%03d.%s", t.Idx, part.ext)] = body
		}
	}
	return out, nil
}

// copyModeChange is the refusal of a reply that changes the mode.
const copyModeChange = "Edit with AI cannot change whether students write a whole program or one function. Use Rewrite with AI and choose there."

// MergeEdit lays the model's reply over the snapshot and checks the result
// with the pack parser and the editor's field rules. Files the reply does not
// carry keep their current content, and a problem.yaml that leaves out keys
// the current one has gets them back (restoreOmitted); the labels of what was
// put back come second. The merged text files (without test data) come back.
// A reply the pack cannot take for a reason asking again will not fix (test
// data, a changed slug, too many files, no change, a changed mode) is an
// *EditPolicyError; one the pack parser refuses is a *PackError listing every
// reason. Nothing is saved.
func (s *Service) MergeEdit(ctx context.Context, snap *EditSnapshot, reply map[string][]byte) (map[string][]byte, []string, error) {
	if len(reply) > MaxEditFiles {
		return nil, nil, &EditPolicyError{Problems: []string{fmt.Sprintf("The model returned more than %d files.", MaxEditFiles)}}
	}
	var reasons []string
	merged := make(map[string][]byte, len(snap.Files)+len(reply))
	for name, body := range snap.Files {
		merged[name] = body
	}
	for _, name := range slices.Sorted(maps.Keys(reply)) {
		if ok, why := editFileName(name); !ok {
			reasons = append(reasons, why)
			continue
		}
		merged[name] = reply[name]
	}
	if len(reasons) > 0 {
		return nil, nil, &EditPolicyError{Problems: reasons}
	}
	var kept []string
	if body, ok := reply["problem.yaml"]; ok {
		body, kept = restoreOmitted(snap.Files["problem.yaml"], body, reply)
		if sameYAML(body, snap.Files["problem.yaml"]) {
			body = snap.Files["problem.yaml"]
		}
		merged["problem.yaml"] = body
		if mode, ok := editMode(body); ok && mode != snap.Mode {
			return nil, nil, &EditPolicyError{Problems: []string{copyModeChange}, ModeChange: true}
		}
	}
	changed := false
	for name, body := range merged {
		if old, had := snap.Files[name]; !had || !bytes.Equal(old, body) {
			changed = true
		}
	}
	if !changed {
		return nil, nil, &EditPolicyError{Problems: []string{"The model did not change anything."}}
	}
	if _, err := s.parseMerged(ctx, snap, merged); err != nil {
		return nil, nil, err
	}
	return merged, kept, nil
}

// parseMerged reads the merged text files with the current tests through the
// real pack parser and checks the slug. It holds the pack slot while the
// test data is in memory.
func (s *Service) parseMerged(ctx context.Context, snap *EditSnapshot, text map[string][]byte) (*Pack, error) {
	release, err := acquirePackSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	p, err := s.Get(ctx, snap.ID)
	if err != nil {
		return nil, err
	}
	tests, err := s.testFiles(p)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(text)+len(tests))
	for name, body := range text {
		files[name] = body
	}
	for name, body := range tests {
		files[name] = body
	}
	pk, err := parsePackFiles(files)
	if err != nil {
		return nil, err
	}
	if pk.Problem.Slug != snap.Slug {
		return nil, &EditPolicyError{Problems: []string{"The slug is " + snap.Slug + " and AI edit cannot change it."}}
	}
	return pk, nil
}

// AcceptEdit saves a merged draft over problem id as one new revision, in
// the way a pack replace does, and marks the problem as an AI draft (origin
// ai, the "ai draft" tag, the solution not written by a person) so a contest
// asks before taking it. baseHash is the content hash the draft was made
// from: a problem that changed since gets ErrEditStale. A running contest
// that holds the problem gives *ReplaceLiveError. The edited problem is not
// validated: Validate has to pass on the new content.
func (s *Service) AcceptEdit(ctx context.Context, id int64, baseHash string, files map[string][]byte, userID int64, instruction string) (*Problem, error) {
	snap, err := s.EditSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if snap.Hash != baseHash {
		return nil, ErrEditStale
	}
	pk, err := s.parseMerged(ctx, snap, files)
	if err != nil {
		return nil, err
	}
	pk.Problem.Origin = OriginAI
	tags := withoutAIDraftTag(append([]string{}, pk.Problem.Tags...))
	if len(tags) >= MaxTags {
		tags = tags[:MaxTags-1]
	}
	pk.Problem.Tags = append(tags, AIDraftTag)
	reason := "AI edit"
	if note := cleanNote(instruction); note != "" {
		reason += ": " + note
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// The check that counts: inside the transaction that writes, so a save
	// or another Accept of this draft that got in first makes this one stale.
	cur, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	curSnap, err := snapshotOf(cur)
	if err != nil {
		return nil, err
	}
	// A proof that finds a changed problem drops its result and queues no
	// validation, so an edit waits for the round: the draft stays for later.
	if open, err := OpenProof(ctx, tx, id); err != nil {
		return nil, err
	} else if open {
		return nil, ErrChecking
	}
	if curSnap.Hash != baseHash {
		return nil, ErrEditStale
	}
	out, err := s.replaceIn(ctx, tx, id, pk, userID, reason)
	if err != nil {
		return nil, err
	}
	// The edit must pass Validate again, whatever it changed.
	if _, err := tx.ExecContext(ctx, "UPDATE problems SET validated_hash = NULL, validated_at = NULL WHERE id = ?", id); err != nil {
		return nil, err
	}
	out.ValidatedHash, out.ValidatedAt = "", time.Time{}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
