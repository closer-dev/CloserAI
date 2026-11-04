package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	_, err := rand.Read(k)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(k)
}

func TestRoundTrip(t *testing.T) {
	c, err := New(newKey(t))
	require.NoError(t, err)

	enc, err := c.Encrypt([]byte("123456:ABC-bot-token"))
	require.NoError(t, err)
	dec, err := c.Decrypt(enc)
	require.NoError(t, err)
	require.Equal(t, "123456:ABC-bot-token", string(dec))
}

func TestWrongKeyFails(t *testing.T) {
	a, _ := New(newKey(t))
	b, _ := New(newKey(t))
	enc, err := a.Encrypt([]byte("secret"))
	require.NoError(t, err)
	_, err = b.Decrypt(enc)
	require.Error(t, err)
}

func TestBadKeyLength(t *testing.T) {
	_, err := New(base64.StdEncoding.EncodeToString([]byte("short")))
	require.Error(t, err)
}
