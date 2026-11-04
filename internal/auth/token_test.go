package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTokenRoundTrip(t *testing.T) {
	tk := newTokens("secret")
	id := Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: RoleOperator}

	raw, err := tk.issue(id)
	require.NoError(t, err)
	got, err := tk.parse(raw)
	require.NoError(t, err)
	require.Equal(t, id, got)
}

func TestTokenWrongSecret(t *testing.T) {
	raw, err := newTokens("a").issue(Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: RoleOwner})
	require.NoError(t, err)
	_, err = newTokens("b").parse(raw)
	require.Error(t, err)
}

func TestTokenExpired(t *testing.T) {
	tk := newTokens("secret")
	raw, err := tk.issue(Identity{UserID: uuid.New(), TenantID: uuid.New(), Role: RoleOwner})
	require.NoError(t, err)

	tk.now = func() time.Time { return time.Now().Add(tokenTTL + time.Minute) }
	_, err = tk.parse(raw)
	require.Error(t, err)
}
