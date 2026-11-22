package domain

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"closerai/internal/kernel"
)

func TestIsStart(t *testing.T) {
	for text, want := range map[string]bool{
		"/start":             true,
		"  /start  ":         true,
		"/start promo_42":    true,
		"/started":           false,
		"привет":             false,
		"как сделать /start": false,
	} {
		require.Equal(t, want, Message{Text: text}.IsStart(), text)
	}
}

func TestGreeting(t *testing.T) {
	require.Equal(t, DefaultGreeting, Greeting("   "))
	require.Equal(t, "Добрый день!", Greeting(" Добрый день! "))
}

func TestTruncate(t *testing.T) {
	short := "привет"
	require.Equal(t, short, Truncate(short))

	long := strings.Repeat("я", MaxTextLen+10)
	got := Truncate(long)
	require.Equal(t, MaxTextLen, utf8.RuneCountInString(got))
	require.True(t, strings.HasSuffix(got, "…"))
}

func TestBotReplies(t *testing.T) {
	require.True(t, Conversation{Status: kernel.StatusBot}.BotReplies())
	for _, s := range []kernel.ConversationStatus{kernel.StatusWaitingOperator, kernel.StatusOperator, kernel.StatusClosed} {
		require.False(t, Conversation{Status: s}.BotReplies(), s)
	}
}
