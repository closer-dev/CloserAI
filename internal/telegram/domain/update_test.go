package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomerMessage(t *testing.T) {
	human := &User{ID: 1, FirstName: "Иван"}
	ok := Message{ID: 5, ChatID: 1, ChatType: ChatPrivate, From: human, Text: "привет"}

	m, accepted := Update{ID: 1, Message: &ok}.CustomerMessage()
	require.True(t, accepted)
	require.Equal(t, ok, m)

	reject := map[string]Update{
		"нет сообщения": {ID: 1},
		"группа":        {ID: 1, Message: &Message{ChatType: "group", From: human, Text: "x"}},
		"без автора":    {ID: 1, Message: &Message{ChatType: ChatPrivate, Text: "x"}},
		"от бота":       {ID: 1, Message: &Message{ChatType: ChatPrivate, From: &User{IsBot: true}, Text: "x"}},
		"без текста":    {ID: 1, Message: &Message{ChatType: ChatPrivate, From: human, Text: "  "}},
	}
	for name, u := range reject {
		_, accepted := u.CustomerMessage()
		require.False(t, accepted, name)
	}
}

func TestSecretMatches(t *testing.T) {
	c := Credentials{WebhookSecret: "abc"}
	require.True(t, c.SecretMatches("abc"))
	require.False(t, c.SecretMatches("abd"))
	require.False(t, c.SecretMatches(""))
	require.False(t, Credentials{}.SecretMatches(""), "пустой секрет не принимается")
}
