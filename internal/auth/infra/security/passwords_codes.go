package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

// Bcrypt хэширует пароли.
type Bcrypt struct{}

func (Bcrypt) Hash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash: %w", err)
	}
	return string(h), nil
}

func (Bcrypt) Matches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HMACCodes генерирует 6-значные коды и хэширует их HMAC-ом. Простой sha256 от
// 6 цифр перебирается за миллисекунды, а без серверного ключа HMAC не подобрать.
type HMACCodes struct {
	key []byte
}

func NewHMACCodes(secret string) *HMACCodes {
	k := sha256.Sum256([]byte("email-code:" + secret))
	return &HMACCodes{key: k[:]}
}

func (c *HMACCodes) Generate() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("auth: generate code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (c *HMACCodes) Hash(code string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(code))
	return hex.EncodeToString(m.Sum(nil))
}

func (c *HMACCodes) Equal(code, hash string) bool {
	return hmac.Equal([]byte(c.Hash(code)), []byte(hash))
}
