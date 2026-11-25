// Package mailer отправляет письма: через SMTP или (в разработке) в лог.
package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"time"
)

type Message struct {
	To      string
	Subject string
	Text    string // текст письма и резервная версия для клиентов без HTML
	HTML    string // необязательная HTML-версия
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender ничего не отправляет, а пишет письмо в лог. Используется, когда SMTP не настроен.
type LogSender struct{}

func (LogSender) Send(ctx context.Context, m Message) error {
	slog.InfoContext(ctx, "email (SMTP не настроен, письмо не отправлено)",
		"to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}

type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

type SMTPSender struct {
	cfg  SMTPConfig
	from *mail.Address
}

func NewSMTP(cfg SMTPConfig) (*SMTPSender, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("mailer: bad SMTP_FROM: %w", err)
	}
	return &SMTPSender{cfg: cfg, from: from}, nil
}

// Send: порт 465 — TLS с самого начала, иначе STARTTLS (обязателен, если задан логин).
func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("mailer: bad recipient: %w", err)
	}
	raw, err := build(s.from, to, m, time.Now())
	if err != nil {
		return fmt.Errorf("mailer: build message: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	tlsCfg := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if s.cfg.Port == 465 {
		conn, err = (&tls.Dialer{Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mailer: dial: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mailer: smtp client: %w", err)
	}
	defer c.Close()

	if s.cfg.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("mailer: starttls: %w", err)
			}
		} else if s.cfg.User != "" {
			return fmt.Errorf("mailer: server does not support STARTTLS, refusing to send password in clear text")
		}
	}
	if s.cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("mailer: auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("mailer: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("mailer: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mailer: send: %w", err)
	}
	return c.Quit()
}

// build собирает текстовое письмо или multipart/alternative, если задан HTML.
func build(from, to *mail.Address, m Message, now time.Time) ([]byte, error) {
	const textType = "text/plain; charset=utf-8"
	const htmlType = "text/html; charset=utf-8"
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.BEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	if m.HTML == "" {
		fmt.Fprintf(&b, "Content-Type: %s\r\nContent-Transfer-Encoding: base64\r\n\r\n", textType)
		err := writeBase64(&b, m.Text)
		return b.Bytes(), err
	}

	w := multipart.NewWriter(&b)
	fmt.Fprintf(&b, "Content-Type: %s\r\n\r\n",
		mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": w.Boundary()}))
	// Клиент выбирает последнюю поддерживаемую версию, поэтому HTML после текста.
	for _, part := range []struct{ contentType, body string }{{textType, m.Text}, {htmlType, m.HTML}} {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Type", part.contentType)
		h.Set("Content-Transfer-Encoding", "base64")
		p, err := w.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if err := writeBase64(p, part.body); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeBase64(w io.Writer, body string) error {
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	for len(enc) > 76 {
		if _, err := io.WriteString(w, enc[:76]+"\r\n"); err != nil {
			return err
		}
		enc = enc[76:]
	}
	_, err := io.WriteString(w, enc+"\r\n")
	return err
}
