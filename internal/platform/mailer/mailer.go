// Package mailer sends transactional email.
package mailer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// Message is a plain-text email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer delivers messages.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends through an SMTP relay without authentication (development relay
// or a local MTA). A provider-specific implementation is added for production.
type SMTP struct {
	Addr string
	From string
}

func (s SMTP) Send(ctx context.Context, m Message) error {
	if strings.ContainsAny(m.To, "\r\n") || strings.ContainsAny(m.Subject, "\r\n") {
		return fmt.Errorf("mailer: header injection attempt")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", s.From)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(m.Text, "\n", "\r\n"))

	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(s.Addr, nil, s.From, []string{m.To}, []byte(b.String())) }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("mailer: send: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Log writes messages to the logger instead of sending them. It never logs
// the body, because bodies contain one-time links.
type Log struct{ Logger *slog.Logger }

func (l Log) Send(ctx context.Context, m Message) error {
	l.Logger.InfoContext(ctx, "email not sent (no SMTP configured)", slog.String("subject", m.Subject))
	return nil
}

// Memory records messages for tests.
type Memory struct {
	mu   sync.Mutex
	Sent []Message
	// Fail makes Send fail (a mail server outage in tests).
	Fail bool
}

func (m *Memory) Send(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail {
		return errors.New("mailer: server unavailable")
	}
	m.Sent = append(m.Sent, msg)
	return nil
}

// SetFail turns the simulated outage on or off.
func (m *Memory) SetFail(v bool) {
	m.mu.Lock()
	m.Fail = v
	m.mu.Unlock()
}

// Last returns the last message sent to addr, if any.
func (m *Memory) Last(addr string) (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.Sent) - 1; i >= 0; i-- {
		if strings.EqualFold(m.Sent[i].To, addr) {
			return m.Sent[i], true
		}
	}
	return Message{}, false
}
