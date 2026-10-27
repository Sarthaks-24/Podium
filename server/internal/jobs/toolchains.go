package jobs

import "github.com/Sarthaks-24/podium/internal/judgeapi"

// RegisterToolchain checks a judge's toolchain report and returns the hash
// the judge sends with each claim. Nothing is stored (v3 dropped the
// registry); the server logs the versions, and install.sh hold keeps the
// compilers from changing during a term.
func (s *Service) RegisterToolchain(t judgeapi.Toolchain) (string, error) {
	if err := t.Check(); err != nil {
		return "", &ReportError{Reason: err.Error()}
	}
	return t.Hash(), nil
}
