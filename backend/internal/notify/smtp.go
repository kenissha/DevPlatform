package notify

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPEmailSender sends real mail through an SMTP server. It is the
// EmailSender this package's design has always pointed at (see
// EmailSender's own doc comment) — NoopEmailSender remains the default
// until an operator sets DEVPLATFORM_SMTP_HOST, at which point main.go
// wires one of these in instead.
//
// STARTTLS is used opportunistically (if the server advertises it) but
// never required: many internal corporate relays only listen on port 25
// with no TLS at all, and refusing to send there would make this sender
// useless on exactly the kind of network this platform runs on. AUTH
// PLAIN is attempted only if Username is set and the server advertises
// AUTH — an anonymous internal relay needs neither.
type SMTPEmailSender struct {
	Host     string
	Port     string
	Username string // empty: no AUTH is attempted
	Password string
	From     string
}

// Send implements EmailSender. subject is always a fixed string from this
// package's own call site (see Store.sendEmail) — never data an external
// caller controls — so it, along with From/to, is safe to place directly
// in a header line without the CRLF-stripping a truly untrusted header
// value would need.
func (s *SMTPEmailSender) Send(to, subject, body string) error {
	addr := net.JoinHostPort(s.Host, s.Port)

	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("notify: dialing smtp server %q: %w", addr, err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
			return fmt.Errorf("notify: smtp starttls: %w", err)
		}
	}

	if s.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("notify: smtp auth: %w", err)
			}
		}
	}

	if err := client.Mail(s.From); err != nil {
		return fmt.Errorf("notify: smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("notify: smtp RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("notify: smtp DATA: %w", err)
	}
	if _, err := w.Write(buildMessage(s.From, to, subject, body)); err != nil {
		_ = w.Close()
		return fmt.Errorf("notify: writing smtp message body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("notify: closing smtp DATA: %w", err)
	}

	return client.Quit()
}

// buildMessage renders a minimal RFC 5322 message. CRLF line endings
// throughout, as the format requires — a bare \n is technically malformed
// and some servers reject it outright.
func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}
