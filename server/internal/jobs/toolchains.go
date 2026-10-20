package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Toolchain is one stored judge toolchain (B1.4, migration 0009).
type Toolchain struct {
	ID        int64              `json:"-"`
	Hash      string             `json:"hash"`
	Details   judgeapi.Toolchain `json:"details"`
	FirstSeen time.Time          `json:"-"`
	LastSeen  time.Time          `json:"-"`
}

// RegisterToolchain stores a judge's toolchain report once per distinct
// value and marks it the latest seen. It returns the hash the judge sends
// with each claim.
func (s *Service) RegisterToolchain(ctx context.Context, t judgeapi.Toolchain) (string, error) {
	if err := t.Check(); err != nil {
		return "", &ReportError{Reason: err.Error()}
	}
	details, _ := json.Marshal(t)
	hash := t.Hash()
	now := s.now().UnixMilli()
	// last_seen only grows, even for two reports in one millisecond, so
	// the latest report is always the current toolchain.
	_, err := s.DB.ExecContext(ctx, `INSERT INTO toolchains (hash, details, first_seen, last_seen)
		VALUES (?1, ?2, ?3, max(?3, (SELECT coalesce(max(last_seen), 0) + 1 FROM toolchains)))
		ON CONFLICT (hash) DO UPDATE SET last_seen = excluded.last_seen`, hash, string(details), now)
	if err != nil {
		return "", err
	}
	// The judge holds the token but is not trusted: keep the table small.
	// A toolchain no job ran on and not among the 20 latest goes.
	_, err = s.DB.ExecContext(ctx, `DELETE FROM toolchains WHERE id NOT IN (SELECT toolchain_id FROM jobs WHERE toolchain_id IS NOT NULL)
		AND id NOT IN (SELECT id FROM toolchains ORDER BY last_seen DESC LIMIT 20)`)
	return hash, err
}

func scanToolchain(row *sql.Row) (*Toolchain, error) {
	var t Toolchain
	var details string
	var first, last int64
	err := row.Scan(&t.ID, &t.Hash, &details, &first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(details), &t.Details); err != nil {
		return nil, err
	}
	t.FirstSeen, t.LastSeen = time.UnixMilli(first), time.UnixMilli(last)
	return &t, nil
}

const toolchainColumns = "t.id, t.hash, t.details, t.first_seen, t.last_seen"

// CurrentToolchain is the toolchain the judge reported last, or nil when
// no judge has reported one.
func (s *Service) CurrentToolchain(ctx context.Context) (*Toolchain, error) {
	return scanToolchain(s.DB.QueryRowContext(ctx, "SELECT "+toolchainColumns+" FROM toolchains t ORDER BY t.last_seen DESC, t.id DESC LIMIT 1"))
}

// ValidatedToolchain is the toolchain of the newest finished validation
// of a problem at contentHash, or nil when that validation ran before the
// registry existed or on a judge that sent no report.
func (s *Service) ValidatedToolchain(ctx context.Context, problemID int64, contentHash string) (*Toolchain, error) {
	return scanToolchain(s.DB.QueryRowContext(ctx, "SELECT "+toolchainColumns+` FROM validations v
		JOIN jobs j ON j.id = v.job_id JOIN toolchains t ON t.id = j.toolchain_id
		WHERE v.problem_id = ? AND v.content_hash = ? AND j.state = 'done' ORDER BY v.id DESC LIMIT 1`, problemID, contentHash))
}
