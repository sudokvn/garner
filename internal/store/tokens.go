package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/sudokvn/garner/internal/store/query"
	"github.com/sudokvn/garner/internal/token"
)

// Token is an authenticated client.
type Token struct {
	ID   int64
	Name string
	hash string
}

// TokenInfo describes a token without revealing it.
type TokenInfo struct {
	Name     string
	Last4    string
	Created  time.Time
	LastUsed time.Time // zero if never used
}

// CreateToken creates a token named name and returns it in plain text. Only
// its hash is stored, so this is the only time it can be shown.
func (s *Store) CreateToken(ctx context.Context, name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	plain, hash, last4 := token.New()
	_, err := s.q.CreateToken(ctx, query.CreateTokenParams{Name: name, Hash: hash, Last4: last4, CreatedAt: time.Now().Unix()})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", fmt.Errorf("token %q: %w", name, ErrExists)
		}
		return "", err
	}
	return plain, nil
}

// Authenticate returns the token matching plain, or ErrUnauthorized.
func (s *Store) Authenticate(ctx context.Context, plain string) (Token, error) {
	if !token.WellFormed(plain) {
		return Token{}, ErrUnauthorized
	}
	row, err := s.q.TokenByHash(ctx, token.Hash(plain))
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrUnauthorized
	}
	if err != nil {
		return Token{}, err
	}
	// Record use at most once a minute to avoid a write per request.
	now := time.Now().Unix()
	if !row.LastUsedAt.Valid || now-row.LastUsedAt.Int64 >= 60 {
		if err := s.q.TouchToken(ctx, query.TouchTokenParams{LastUsedAt: sql.NullInt64{Int64: now, Valid: true}, ID: row.ID}); err != nil {
			s.log.Warn("could not record token use", "token", row.Name, "err", err)
		}
	}
	return Token{ID: row.ID, Name: row.Name, hash: token.Hash(plain)}, nil
}

// StillValid reports whether t has not been revoked since it authenticated.
func (s *Store) StillValid(ctx context.Context, t Token) (bool, error) {
	n, err := s.q.TokenStillValid(ctx, query.TokenStillValidParams{ID: t.ID, Hash: t.hash})
	return n == 1, err
}

func (s *Store) Tokens(ctx context.Context) ([]TokenInfo, error) {
	rows, err := s.q.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TokenInfo, len(rows))
	for i, r := range rows {
		out[i] = TokenInfo{Name: r.Name, Last4: r.Last4, Created: time.Unix(r.CreatedAt, 0)}
		if r.LastUsedAt.Valid {
			out[i].LastUsed = time.Unix(r.LastUsedAt.Int64, 0)
		}
	}
	return out, nil
}

// RevokeToken deletes the token named name and returns its ID.
func (s *Store) RevokeToken(ctx context.Context, name string) (int64, error) {
	id, err := s.q.DeleteToken(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("token %q: %w", name, ErrNotFound)
	}
	return id, err
}

func validName(name string) error {
	if name == "" || len(name) > 64 {
		return errors.New("token name must be 1 to 64 characters")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_.@", r) {
			return fmt.Errorf("token name may contain letters, digits and - _ . @, not %q", r)
		}
	}
	return nil
}
