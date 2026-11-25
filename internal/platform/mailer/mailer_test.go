package mailer

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
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
	raw, err := build(from, to, Message{Subject: "Код подтверждения", Text: text}, time.Unix(0, 0))
	require.NoError(t, err)

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)

	subj, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "Код подтверждения", subj)
	require.Equal(t, "<user@example.com>", msg.Header.Get("To"))
	require.Equal(t, "text/plain; charset=utf-8", msg.Header.Get("Content-Type"))
	require.Equal(t, "base64", msg.Header.Get("Content-Transfer-Encoding"))

	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	require.NoError(t, err)
	require.Equal(t, text, string(body))

	for _, line := range strings.Split(string(raw), "\r\n") {
		require.LessOrEqual(t, len(line), 998, "строки письма не должны превышать лимит RFC 5322")
	}
}

func TestBuildMultipartAlternative(t *testing.T) {
	from := &mail.Address{Name: "CloserAI", Address: "noreply@example.com"}
	to := &mail.Address{Address: "user@example.com"}
	m := Message{Subject: "Код подтверждения", Text: "Ваш код: 001234", HTML: "<p>Ваш код: <b>001234</b></p>"}
	raw, err := build(from, to, m, time.Unix(0, 0))
	require.NoError(t, err)
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)
	require.NotEmpty(t, params["boundary"])

	r := multipart.NewReader(msg.Body, params["boundary"])
	for _, want := range []struct{ mediaType, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		p, err := r.NextPart()
		require.NoError(t, err)
		partType, partParams, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, want.mediaType, partType)
		require.Equal(t, "utf-8", partParams["charset"])
		require.Equal(t, "base64", p.Header.Get("Content-Transfer-Encoding"))
		body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		require.NoError(t, err)
		require.Equal(t, want.body, string(body))
	}
	_, err = r.NextPart()
	require.ErrorIs(t, err, io.EOF)
}
