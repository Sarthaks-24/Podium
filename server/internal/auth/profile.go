package auth

import "context"

// Profile is what the person's own /me page shows beside the session's
// User (v4 M1): when the account was made and when it last signed in, in
// Unix milliseconds; LastSignIn is 0 for an account that never signed in.
type Profile struct {
	MemberSince int64 `json:"member_since"`
	LastSignIn  int64 `json:"last_sign_in"`
}

// Profile reads the person's own dates; nobody else's are ever asked for.
func (s *Service) Profile(ctx context.Context, userID int64) (Profile, error) {
	var p Profile
	err := s.DB.QueryRowContext(ctx, "SELECT created_at, coalesce(last_login_at, 0) FROM users WHERE id = ?", userID).
		Scan(&p.MemberSince, &p.LastSignIn)
	return p, err
}
