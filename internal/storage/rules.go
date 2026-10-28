package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The kinds of standing rule.
const (
	// RuleInstruction is an order for the bot in one group, such as "do not
	// talk about finance".
	RuleInstruction = "instruction"
	// RuleIgnore names a member the bot does not answer in one group.
	RuleIgnore = "ignore"
	// RuleAddress is how the bot addresses one owner, everywhere.
	RuleAddress = "address"
	// RuleSwearGuard has the group's swearing warned about, then muted, in
	// code; its UserName is whom a mute tags, "" for the group's creator.
	RuleSwearGuard = "swear_guard"
	// RuleWelcome is what the bot says to new members of one group, in an
	// owner's words; "{name}" in it is the newcomers' names.
	RuleWelcome = "welcome"
	// RuleSpamGuard bans members known as spammers, RuleLinkGuard deletes
	// links to malicious hosts and RuleImageGuard nude or gory pictures, in
	// one group, in code.
	RuleSpamGuard  = "spam_guard"
	RuleLinkGuard  = "link_guard"
	RuleImageGuard = "image_guard"
	// RuleReplyLimit and RuleMinActive are an owner's settings for one
	// group, the number in Text: the answers a minute the bot gives there,
	// and how many members must have written lately for it to answer at all,
	// 0 for none. Without one the configured default holds.
	RuleReplyLimit = "reply_limit"
	RuleMinActive  = "min_active"
)

// MaxInstructions is how many instructions a group may have in force at
// once. Every one of them is sent with every turn there.
const MaxInstructions = 20

// ErrTooManyRules means the group already has MaxInstructions in force.
var ErrTooManyRules = errors.New("storage: too many standing instructions in this chat")

// GroupRule is a standing order from an owner, or for an ignore from a
// group's admin. It lasts until it expires or is removed; clearing a chat's
// history does not touch it.
type GroupRule struct {
	ID, ChatID int64
	Kind       string
	// UserID is the ignored member, the owner an address is for, or the
	// member an instruction is about; zero for an instruction about no one.
	UserID int64
	// UserName names UserID in lists and in the turn header.
	UserName string
	Text     string
	SetBy    int64
	// CreatedAt is set by the store. A zero ExpiresAt means never.
	CreatedAt, ExpiresAt time.Time
}

// SetGroupRule saves a rule and returns its ID. An ignore or an address
// replaces the one before it for the same person, so that saying it again
// changes it rather than piling up copies. An instruction is refused with
// ErrTooManyRules when the chat already has MaxInstructions in force. A
// per-chat lock makes the check and the insert one step.
func (s *Store) SetGroupRule(ctx context.Context, r GroupRule) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, r.ChatID^0x52554c45); err != nil { // "RULE"
			return err
		}
		if err := makeRoom(ctx, tx, r); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO group_rules (chat_id, kind, user_id, user_name, text, set_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id`,
			r.ChatID, r.Kind, nonZero(r.UserID), r.UserName, r.Text, r.SetBy, nullableTime(r.ExpiresAt)).Scan(&id)
	})
	if errors.Is(err, ErrTooManyRules) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("storage: set group rule: %w", err)
	}
	return id, nil
}

// makeRoom clears the way for r: it drops the rule r replaces, or refuses an
// instruction over the limit.
func makeRoom(ctx context.Context, tx pgx.Tx, r GroupRule) error {
	if r.Kind != RuleInstruction {
		_, err := tx.Exec(ctx, `
			DELETE FROM group_rules WHERE chat_id = $1 AND kind = $2 AND user_id IS NOT DISTINCT FROM $3`,
			r.ChatID, r.Kind, nonZero(r.UserID))
		return err
	}
	var inForce int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM group_rules
		WHERE chat_id = $1 AND kind = 'instruction' AND (expires_at IS NULL OR expires_at > now())`, r.ChatID).Scan(&inForce)
	if err != nil {
		return err
	}
	if inForce >= MaxInstructions {
		return ErrTooManyRules
	}
	return nil
}

// GroupRules returns the rules in force in a chat, together with those that
// hold everywhere, oldest first.
func (s *Store) GroupRules(ctx context.Context, chatID int64) ([]GroupRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, chat_id, kind, coalesce(user_id, 0), user_name, text, set_by, created_at, expires_at
		FROM group_rules
		WHERE chat_id IN ($1, 0) AND (expires_at IS NULL OR expires_at > now())
		ORDER BY id`, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: read group rules: %w", err)
	}
	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupRule, error) {
		var (
			r       GroupRule
			expires *time.Time
		)
		err := row.Scan(&r.ID, &r.ChatID, &r.Kind, &r.UserID, &r.UserName, &r.Text, &r.SetBy, &r.CreatedAt, &expires)
		if expires != nil {
			r.ExpiresAt = *expires
		}
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read group rules: %w", err)
	}
	return rules, nil
}

// RemoveGroupRule deletes a rule in force, by its ID, and reports whether
// there was one. Only a rule GroupRules lists for the chat can be removed
// from it: the chat's own, or one that holds everywhere.
func (s *Store) RemoveGroupRule(ctx context.Context, chatID, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM group_rules
		WHERE id = $2 AND chat_id IN ($1, 0) AND (expires_at IS NULL OR expires_at > now())`, chatID, id)
	if err != nil {
		return false, fmt.Errorf("storage: remove group rule: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RemoveIgnore stops ignoring a member in a chat, and reports whether they
// were ignored there.
func (s *Store) RemoveIgnore(ctx context.Context, chatID, userID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM group_rules
		WHERE chat_id = $1 AND kind = 'ignore' AND user_id = $2 AND (expires_at IS NULL OR expires_at > now())`, chatID, userID)
	if err != nil {
		return false, fmt.Errorf("storage: remove ignore: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// nullableTime stores a zero time as NULL.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// ClearGroupSetting removes a group's setting of kind, so that the default
// holds there again.
func (s *Store) ClearGroupSetting(ctx context.Context, chatID int64, kind string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM group_rules WHERE chat_id = $1 AND kind = $2`, chatID, kind); err != nil {
		return fmt.Errorf("storage: clear group setting: %w", err)
	}
	return nil
}
