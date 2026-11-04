package mailer

import (
	"encoding/base64"
	"io"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuild(t *testing.T) {
	from, _ := mail.ParseAddress("CloserAI <noreply@example.com>")
	to, _ := mail.ParseAddress("user@example.com")
	text := strings.Repeat("Ваш код подтверждения: 123456. ", 5)
	raw := build(from, to, Message{Subject: "Код подтверждения", Text: text}, time.Unix(0, 0))

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	require.NoError(t, err)

	subj, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "Код подтверждения", subj)
	require.Equal(t, "<user@example.com>", msg.Header.Get("To"))

	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding,
		strings.NewReader(strings.ReplaceAll(mustRead(t, msg.Body), "\r\n", ""))))
	require.NoError(t, err)
	require.Equal(t, text, string(body))

	for _, line := range strings.Split(string(raw), "\r\n") {
		require.LessOrEqual(t, len(line), 998, "строки письма не должны превышать лимит RFC 5322")
	}
}

func mustRead(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}
