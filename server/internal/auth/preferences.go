package auth

import (
	"context"
	"regexp"
)

// Verdict colour sets (decision D40).
const (
	VerdictsStandard = "standard"
	VerdictsSignal   = "signal"
)

// themeID matches a theme id from the web app's list. The server checks
// only the shape: the web app owns the list and ignores ids it does not
// know, so a theme can be added or renamed without a server change.
var themeID = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Preferences are one person's display settings. An empty Theme means
// "follow the system".
type Preferences struct {
	Theme    string `json:"theme"`
	Verdicts string `json:"verdicts"`
}

// SetPreferences saves p for the user. It refuses a malformed theme id or
// an unknown verdict set.
func (s *Service) SetPreferences(ctx context.Context, userID int64, p Preferences) error {
	if p.Theme != "" && !themeID.MatchString(p.Theme) {
		return invalid("Choose a theme from the list.")
	}
	if p.Verdicts != VerdictsStandard && p.Verdicts != VerdictsSignal {
		return invalid("Choose standard or colour-blind safe verdict colours.")
	}
	res, err := s.DB.ExecContext(ctx, "UPDATE users SET theme = ?, verdicts = ? WHERE id = ?",
		p.Theme, p.Verdicts, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("That account no longer exists.")
	}
	return nil
}
