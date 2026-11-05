package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
)

// Verdict colour sets.
const (
	VerdictsStandard = "standard"
	VerdictsSignal   = "signal"
)

// themeID matches a theme id from the web app's list. The server checks
// only the shape: the web app owns the list and ignores ids it does not
// know, so a theme can be added or renamed without a server change.
var themeID = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// EditorColourIDs are the editor colour schemes the web app ships (lib/editor-colours.ts).
// Unlike theme ids the server checks the list, because a saved id is applied
// as an attribute and the list is short and fixed.
var EditorColourIDs = []string{"night", "paper", "midnight", "dusk", "high-contrast"}

// hexColour is #RRGGBB, either case.
var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// CustomTheme is the three inputs of a custom interface theme. The browser
// derives every colour from them (lib/theme-engine.ts); the server never
// does, it only checks the shape.
type CustomTheme struct {
	Background string `json:"background"`
	Accent     string `json:"accent"`
	Contrast   string `json:"contrast"`
}

// Preferences are one person's display settings. An empty Theme means
// "follow the system". EditorColours and ThemeCustom are optional: a
// request that leaves them out keeps the saved values, so a browser still
// running an older copy of the page cannot wipe them.
type Preferences struct {
	Theme         string          `json:"theme"`
	Verdicts      string          `json:"verdicts"`
	EditorColours *string         `json:"editor_colours,omitempty"`
	ThemeCustom   json.RawMessage `json:"theme_custom,omitempty"`
}

// customJSON turns the stored column into the API value: nil for none.
func customJSON(stored string) json.RawMessage {
	if stored == "" {
		return nil
	}
	return json.RawMessage(stored)
}

// parseCustom checks a custom theme's JSON and returns its canonical text
// (lowercase colours, fixed key order) for storage.
func parseCustom(raw json.RawMessage) (string, error) {
	var c CustomTheme
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil || dec.More() {
		return "", errors.New("shape")
	}
	if !hexColour.MatchString(c.Background) || !hexColour.MatchString(c.Accent) {
		return "", errors.New("colour")
	}
	if c.Contrast != "standard" && c.Contrast != "high" {
		return "", errors.New("contrast")
	}
	c.Background, c.Accent = lowerHex(c.Background), lowerHex(c.Accent)
	b, err := json.Marshal(c)
	return string(b), err
}

func lowerHex(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'F' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// SetPreferences saves p for the user. It refuses a malformed theme id, an
// unknown verdict set or editor scheme, and a custom theme that is not two
// #RRGGBB colours and a contrast word, naming the field in a FieldErrors.
// A theme of "custom" needs a saved custom theme.
func (s *Service) SetPreferences(ctx context.Context, userID int64, p Preferences) error {
	fields := FieldErrors{}
	if p.Theme != "" && !themeID.MatchString(p.Theme) {
		fields["theme"] = "Choose a theme from the list."
	}
	if p.Verdicts != VerdictsStandard && p.Verdicts != VerdictsSignal {
		fields["verdicts"] = "Choose standard or colour-blind safe verdict colours."
	}
	if p.EditorColours != nil {
		known := false
		for _, id := range EditorColourIDs {
			known = known || id == *p.EditorColours
		}
		if !known {
			fields["editor_colours"] = "Choose an editor colour scheme from the list."
		}
	}
	custom, haveCustom := "", false
	if len(p.ThemeCustom) > 0 && string(p.ThemeCustom) != "null" {
		c, err := parseCustom(p.ThemeCustom)
		if err != nil {
			fields["theme_custom"] = "A custom theme needs a background colour, an accent colour (each as #RRGGBB) and a contrast of standard or high."
		}
		custom, haveCustom = c, err == nil
	}
	if len(fields) > 0 {
		return fields
	}

	// "custom" with nothing in the request needs a theme saved earlier.
	if p.Theme == "custom" && !haveCustom {
		var saved string
		err := s.DB.QueryRowContext(ctx, "SELECT theme_custom FROM users WHERE id = ?", userID).Scan(&saved)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if saved == "" {
			return FieldErrors{"theme_custom": "Build a custom theme before choosing it."}
		}
	}

	// Absent optional fields keep the saved value (COALESCE on a NULL parameter).
	var editor, saved any
	if p.EditorColours != nil {
		editor = *p.EditorColours
	}
	if haveCustom {
		saved = custom
	} else if string(p.ThemeCustom) == "null" {
		saved = ""
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE users SET theme = ?, verdicts = ?,
		editor_colours = COALESCE(?, editor_colours), theme_custom = COALESCE(?, theme_custom) WHERE id = ?`,
		p.Theme, p.Verdicts, editor, saved, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("That account no longer exists.")
	}
	return nil
}
