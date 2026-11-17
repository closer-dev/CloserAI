// Package security — реализации криптографических портов auth: JWT-сессии,
// bcrypt-пароли, HMAC-хэши одноразовых кодов.
package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"closerai/internal/kernel"
)

const TokenTTL = 7 * 24 * time.Hour

type claims struct {
	TenantID      uuid.UUID   `json:"tid"`
	Role          kernel.Role `json:"role"`
	EmailVerified bool        `json:"ev"`
	jwt.RegisteredClaims
}

// JWT выпускает и проверяет сессионные токены (HS256).
type JWT struct {
	secret []byte
	now    func() time.Time
}

func NewJWT(secret string) *JWT {
	return &JWT{secret: []byte(secret), now: time.Now}
}

func (t *JWT) Issue(id kernel.Identity) (string, error) {
	now := t.now()
	c := claims{
		TenantID:      id.TenantID,
		Role:          id.Role,
		EmailVerified: id.EmailVerified,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return s, nil
}

func (t *JWT) Parse(raw string) (kernel.Identity, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(t.now),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return kernel.Identity{}, fmt.Errorf("auth: parse token: %w", err)
	}
	uid, err := uuid.Parse(c.Subject)
	if err != nil {
		return kernel.Identity{}, fmt.Errorf("auth: bad subject: %w", err)
	}
	if c.TenantID == uuid.Nil || !c.Role.Valid() {
		return kernel.Identity{}, errors.New("auth: incomplete claims")
	}
	return kernel.Identity{UserID: uid, TenantID: c.TenantID, Role: c.Role, EmailVerified: c.EmailVerified}, nil
}
