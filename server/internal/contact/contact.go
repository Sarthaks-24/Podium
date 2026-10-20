// Package contact keeps the messages signed-in people send to the
// organisers through the contact form (contract C6, decision D71 as
// amended): stored as plain text in contact_messages, listed for owners
// and admins, deleted on request (recorded in the site audit), by the
// retention job after 180 days, and when the sender is erased. There is
// no mail server; organisers reply from their own mail with a mailto link.
package contact

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// Limits on the form.
const (
	// MaxMessage is the longest message, in characters.
	MaxMessage = 2000
	// PerUserPerDay is how many messages one person may send in 24 hours.
	PerUserPerDay = 5
	// SitePerDay is how many messages everyone together may send in 24
	// hours, so one bad night cannot bury the inbox.
	SitePerDay = 100
	// Page is how many messages one inbox page holds.
	Page = 50
)

// AuditDeleted is the site audit action for a deleted message.
const AuditDeleted = "contact_deleted"

// Topics a message may have.
var Topics = []string{"contest", "account", "privacy", "other"}

// Errors the form returns.
var (
	// ErrUserLimit: this person sent PerUserPerDay messages in 24 hours.
	ErrUserLimit = errors.New("contact: too many messages from this person today")
	// ErrSiteLimit: SitePerDay messages arrived in 24 hours.
	ErrSiteLimit = errors.New("contact: too many messages today")
	// ErrNotFound: no such message.
	ErrNotFound = errors.New("contact: no such message")
)

// FieldErrors names the fields of a refused message.
type FieldErrors = auth.FieldErrors

// Service stores and lists messages.
type Service struct {
	DB *sql.DB
	// Auth writes the site audit row of a delete.
	Auth *auth.Service
	// Now is the clock; tests replace it.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Sender is the account a message came from, as it is now.
type Sender struct {
	UserID int64     `json:"user_id"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Role   auth.Role `json:"role"`
}

// Message is one message in the inbox.
type Message struct {
	ID        int64  `json:"id"`
	Topic     string `json:"topic"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at"`
	Sender    Sender `json:"sender"`
}

// check trims the message and returns what is wrong with the request.
func check(topic, message string) (string, FieldErrors) {
	f := FieldErrors{}
	known := false
	for _, t := range Topics {
		known = known || t == topic
	}
	if !known {
		f["topic"] = "Choose what the message is about."
	}
	message = strings.TrimSpace(message)
	switch {
	case message == "":
		f["message"] = "Write your message."
	case !utf8.ValidString(message):
		f["message"] = "The message has characters that are not text. Type it again."
	case utf8.RuneCountInString(message) > MaxMessage:
		f["message"] = "Keep the message to 2,000 characters."
	default:
		for _, r := range message {
			if r != '\n' && r != '\t' && r != '\r' && (unicode.IsControl(r) || auth.Hidden(r)) {
				f["message"] = "The message has characters that are not text. Type it again."
				break
			}
		}
	}
	if len(f) == 0 {
		return message, nil
	}
	return message, f
}

// Send stores a message from userID. It refuses a person's sixth message
// in 24 hours and the site's 101st. The count and the insert are one
// statement, so two sends at once cannot both pass the last free place.
func (s *Service) Send(ctx context.Context, userID int64, topic, message string) (id, createdAt int64, err error) {
	message, f := check(topic, message)
	if f != nil {
		return 0, 0, f
	}
	now := s.now().UnixMilli()
	since := now - (24 * time.Hour).Milliseconds()
	err = s.DB.QueryRowContext(ctx, `INSERT INTO contact_messages (user_id, topic, message, created_at)
		SELECT ?, ?, ?, ?
		WHERE (SELECT count(*) FROM contact_messages WHERE user_id = ? AND created_at > ?) < ?
		  AND (SELECT count(*) FROM contact_messages WHERE created_at > ?) < ?
		RETURNING id`, userID, topic, message, now, userID, since, PerUserPerDay, since, SitePerDay).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var mine int
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM contact_messages WHERE user_id = ? AND created_at > ?",
			userID, since).Scan(&mine); err != nil {
			return 0, 0, err
		}
		if mine >= PerUserPerDay {
			return 0, 0, ErrUserLimit
		}
		return 0, 0, ErrSiteLimit
	}
	if err != nil {
		return 0, 0, err
	}
	return id, now, nil
}

// List returns a page of messages, newest first, older than before when
// before is set, and whether there are more.
func (s *Service) List(ctx context.Context, before int64) ([]Message, bool, error) {
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id, m.topic, m.message, m.created_at, u.id, u.email, u.name, u.role
		FROM contact_messages m JOIN users u ON u.id = m.user_id WHERE m.id < ? ORDER BY m.id DESC LIMIT ?`, before, Page+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Topic, &m.Message, &m.CreatedAt, &m.Sender.UserID, &m.Sender.Email, &m.Sender.Name,
			&m.Sender.Role); err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > Page
	if more {
		out = out[:Page]
	}
	return out, more, nil
}

// Delete removes one message for the actor on ctx, with a site audit row
// naming the sender and the topic, never the text.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var topic, email string
	err = tx.QueryRowContext(ctx, `DELETE FROM contact_messages WHERE id = ?
		RETURNING topic, (SELECT email FROM users WHERE users.id = contact_messages.user_id)`, id).Scan(&topic, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := s.Auth.RecordSiteTx(ctx, tx, AuditDeleted, email, map[string]any{"message_id": id, "topic": topic}); err != nil {
		return err
	}
	return tx.Commit()
}
