package domain

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const validToken = "123456789:AAFakeTokenFakeTokenFakeToken_-12345"

func TestNormalizeToken(t *testing.T) {
	tok, err := NormalizeToken("  " + validToken + "\n")
	require.NoError(t, err)
	require.Equal(t, validToken, tok)

	for _, bad := range []string{"", "abc", "123:short", "abc:AAFakeTokenFakeTokenFakeToken_-12345", validToken + " x"} {
		_, err := NormalizeToken(bad)
		require.ErrorIs(t, err, ErrInvalidToken, bad)
	}
}

func TestSettingsApply(t *testing.T) {
	prompt, greet, thr := "  Ты помощник магазина  ", "Здравствуйте!", float32(0.7)
	s, err := DefaultSettings().Apply(SettingsPatch{SystemPrompt: &prompt, Greeting: &greet, HandoffThreshold: &thr})
	require.NoError(t, err)
	require.Equal(t, Settings{SystemPrompt: "Ты помощник магазина", Greeting: greet, HandoffThreshold: 0.7}, s)

	// пустой патч ничего не меняет
	same, err := s.Apply(SettingsPatch{})
	require.NoError(t, err)
	require.Equal(t, s, same)
}

func TestSettingsValidate(t *testing.T) {
	long := strings.Repeat("я", MaxSystemPromptLen+1) // считаем символы, а не байты
	_, err := DefaultSettings().Apply(SettingsPatch{SystemPrompt: &long})
	require.ErrorIs(t, err, ErrPromptTooLong)

	ok := strings.Repeat("я", MaxSystemPromptLen)
	_, err = DefaultSettings().Apply(SettingsPatch{SystemPrompt: &ok})
	require.NoError(t, err)

	longGreet := strings.Repeat("a", MaxGreetingLen+1)
	_, err = DefaultSettings().Apply(SettingsPatch{Greeting: &longGreet})
	require.ErrorIs(t, err, ErrGreetingTooLong)

	for _, v := range []float32{-0.1, 1.01, float32(math.NaN())} {
		_, err = DefaultSettings().Apply(SettingsPatch{HandoffThreshold: &v})
		require.ErrorIs(t, err, ErrInvalidThreshold)
	}
}

func TestNewWebhookSecret(t *testing.T) {
	a, err := NewWebhookSecret()
	require.NoError(t, err)
	b, _ := NewWebhookSecret()
	require.NotEqual(t, a, b)
	require.Regexp(t, `^[A-Za-z0-9_-]{1,256}$`, a)
}
