package auth

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCodeFormatAndHash(t *testing.T) {
	c := newCodes("secret")
	for range 100 {
		code, err := c.generate()
		require.NoError(t, err)
		require.Regexp(t, `^\d{6}$`, code)
		h := c.hash(code)
		require.True(t, c.equal(code, h))
		require.NotEqual(t, code, h)
	}
	require.False(t, c.equal("000000", c.hash("000001")))
	// другой секрет — другой хэш
	require.NotEqual(t, c.hash("123456"), newCodes("other").hash("123456"))
}

func TestTokenCarriesEmailVerified(t *testing.T) {
	tk := newTokens("secret")
	for _, v := range []bool{true, false} {
		raw, err := tk.issue(Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: RoleOwner, EmailVerified: v})
		require.NoError(t, err)
		id, err := tk.parse(raw)
		require.NoError(t, err)
		require.Equal(t, v, id.EmailVerified)
	}
}
