package security

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/kernel"
)

func TestTokenRoundTrip(t *testing.T) {
	tk := NewJWT("secret")
	id := kernel.Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: kernel.RoleOperator}

	raw, err := tk.Issue(id)
	require.NoError(t, err)
	got, err := tk.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, id, got)
}

func TestTokenWrongSecret(t *testing.T) {
	raw, err := NewJWT("a").Issue(kernel.Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: kernel.RoleOwner})
	require.NoError(t, err)
	_, err = NewJWT("b").Parse(raw)
	require.Error(t, err)
}

func TestTokenExpired(t *testing.T) {
	tk := NewJWT("secret")
	raw, err := tk.Issue(kernel.Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: kernel.RoleOwner})
	require.NoError(t, err)

	tk.now = func() time.Time { return time.Now().Add(TokenTTL + time.Minute) }
	_, err = tk.Parse(raw)
	require.Error(t, err)
}

func TestTokenCarriesEmailVerified(t *testing.T) {
	tk := NewJWT("secret")
	for _, v := range []bool{true, false} {
		raw, err := tk.Issue(kernel.Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: kernel.RoleOwner, EmailVerified: v})
		require.NoError(t, err)
		id, err := tk.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, v, id.EmailVerified)
	}
}

func TestCodeFormatAndHash(t *testing.T) {
	c := NewHMACCodes("secret")
	for range 100 {
		code, err := c.Generate()
		require.NoError(t, err)
		require.Regexp(t, `^\d{6}$`, code)
		h := c.Hash(code)
		require.True(t, c.Equal(code, h))
		require.NotEqual(t, code, h)
	}
	require.False(t, c.Equal("000000", c.Hash("000001")))
	// другой секрет — другой хэш
	require.NotEqual(t, c.Hash("123456"), NewHMACCodes("other").Hash("123456"))
}

func TestBcrypt(t *testing.T) {
	h, err := Bcrypt{}.Hash("password1")
	require.NoError(t, err)
	require.True(t, Bcrypt{}.Matches(h, "password1"))
	require.False(t, Bcrypt{}.Matches(h, "password2"))
}
