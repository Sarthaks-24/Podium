package problems

// Test helpers: revision lookups the server no longer needs.

import "context"

// CurrentRevision returns the problem's newest revision.
func (s *Service) CurrentRevision(ctx context.Context, problemID int64) (*Revision, error) {
	r, err := latestRevision(ctx, s.DB, problemID)
	if err == nil && r == nil {
		return nil, ErrNotFound
	}
	return r, err
}

// RevisionByNumber returns revision rev of a problem.
func (s *Service) RevisionByNumber(ctx context.Context, problemID int64, rev int) (*Revision, error) {
	return scanRevision(s.DB.QueryRowContext(ctx, "SELECT "+revisionColumns+" FROM problem_revisions WHERE problem_id = ? AND rev = ?", problemID, rev))
}
