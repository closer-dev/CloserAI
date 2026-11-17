package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEmailCodeRules(t *testing.T) {
	now := time.Now()
	c := NewEmailCode(uuid.New(), PurposeVerifyEmail, "h", now)
	c.CreatedAt = now

	require.True(t, c.Usable(now))
	require.False(t, c.Expired(now))
	require.True(t, c.Expired(now.Add(CodeTTL+time.Second)))
	require.False(t, c.Usable(now.Add(CodeTTL+time.Second)))

	c.Attempts = CodeMaxAttempts
	require.False(t, c.Usable(now))

	require.Equal(t, CodeResendCooldown, c.ResendWait(now))
	require.Zero(t, c.ResendWait(now.Add(CodeResendCooldown)))
}
