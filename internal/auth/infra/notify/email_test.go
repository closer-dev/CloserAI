package notify

import (
	"context"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"closerai/internal/auth/domain"
	"closerai/internal/platform/mailer"
)

type captureSender struct {
	message mailer.Message
}

func (s *captureSender) Send(_ context.Context, m mailer.Message) error {
	s.message = m
	return nil
}

func TestEmbeddedVerifyCodeTemplates(t *testing.T) {
	sender := &captureSender{}
	e, err := NewEmail(sender)
	require.NoError(t, err)
	u := domain.User{Name: "Анна", Email: "user@example.com"}
	const code = "001234"
	require.NoError(t, e.SendVerifyCode(t.Context(), u, code))
	require.Equal(t, u.Email, sender.message.To)
	require.Equal(t, "Код подтверждения CloserAI", sender.message.Subject)
	require.Contains(t, sender.message.Text, u.Name)
	require.Contains(t, sender.message.Text, code)
	require.Contains(t, sender.message.Text, strconv.Itoa(int(domain.CodeTTL/time.Minute))+" минут")
	if e.html != nil {
		require.Contains(t, sender.message.HTML, code)
	}
}

func TestVerifyCodeWithoutHTML(t *testing.T) {
	sender := &captureSender{}
	e, err := newEmail(sender, fstest.MapFS{verifyText: {Data: []byte("Код: {{.Code}}")}})
	require.NoError(t, err)
	require.NoError(t, e.SendVerifyCode(t.Context(), domain.User{Email: "user@example.com"}, "001234"))
	require.Equal(t, "Код: 001234", sender.message.Text)
	require.Empty(t, sender.message.HTML)
}

func TestVerifyCodeWithHTML(t *testing.T) {
	sender := &captureSender{}
	e, err := newEmail(sender, fstest.MapFS{
		verifyText: {Data: []byte("Код: {{.Code}}")},
		verifyHTML: {Data: []byte("<p>{{.Name}}: <strong>{{.Code}}</strong> ({{.TTLMinutes}} мин.)</p>")},
	})
	require.NoError(t, err)
	u := domain.User{Name: "Анна <script>", Email: "user@example.com"}
	require.NoError(t, e.SendVerifyCode(t.Context(), u, "001234"))
	require.Equal(t, "Код: 001234", sender.message.Text)
	require.Equal(t, "<p>Анна &lt;script&gt;: <strong>001234</strong> ("+
		strconv.Itoa(int(domain.CodeTTL/time.Minute))+" мин.)</p>", sender.message.HTML)
}

func TestInvalidVerifyHTML(t *testing.T) {
	_, err := newEmail(&captureSender{}, fstest.MapFS{
		verifyText: {Data: []byte("Код: {{.Code}}")},
		verifyHTML: {Data: []byte("<p>{{.Code</p>")},
	})
	require.ErrorContains(t, err, verifyHTML)
}
