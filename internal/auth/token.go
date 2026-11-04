package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const tokenTTL = 7 * 24 * time.Hour

type claims struct {
	TenantID      uuid.UUID `json:"tid"`
	Role          Role      `json:"role"`
	EmailVerified bool      `json:"ev"`
	jwt.RegisteredClaims
}

type tokens struct {
	secret []byte
	now    func() time.Time
}

func newTokens(secret string) *tokens {
	return &tokens{secret: []byte(secret), now: time.Now}
}

func (t *tokens) issue(id Identity) (string, error) {
	now := t.now()
	c := claims{
		TenantID:      id.TenantID,
		Role:          id.Role,
		EmailVerified: id.EmailVerified,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return s, nil
}

func (t *tokens) parse(raw string) (Identity, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(t.now),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("auth: parse token: %w", err)
	}
	uid, err := uuid.Parse(c.Subject)
	if err != nil {
		return Identity{}, fmt.Errorf("auth: bad subject: %w", err)
	}
	if c.TenantID == uuid.Nil || !c.Role.Valid() {
		return Identity{}, errors.New("auth: incomplete claims")
	}
	return Identity{UserID: uid, TenantID: c.TenantID, Role: c.Role, EmailVerified: c.EmailVerified}, nil
}
