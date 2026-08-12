package notify

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTPServer is a minimal SMTP server that speaks just enough of the
// protocol to exercise SMTPEmailSender.Send for real over a TCP socket —
// this is the same "drive the real client against a fake peer" approach
// internal/deploy's fakeCommandRunner and this codebase's httptest-based
// gitserver tests already use, applied to SMTP instead of a CommandRunner
// or HTTP handler.
//
// STARTTLS is deliberately not implemented here: upgrading the connection
// mid-stream needs a self-signed cert and a TLS server handshake, real
// complexity for a three-line call (client.StartTLS) that is itself
// exercised by net/smtp's and crypto/tls's own extensive standard-library
// test suites. What this package's own code contributes — deciding
// whether to call it, choosing AUTH, building the message — is what these
// tests cover.
type fakeSMTPServer struct {
	authAdvertised bool

	heloName string
	authLine string
	mailFrom string
	rcptTo   string
	data     string
}

func startFakeSMTPServer(t *testing.T, advertiseAuth bool) (addr string, server *fakeSMTPServer) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	server = &fakeSMTPServer{authAdvertised: advertiseAuth}

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed by t.Cleanup
		}
		defer conn.Close()
		server.serve(conn)
	}()

	return ln.Addr().String(), server
}

func (s *fakeSMTPServer) serve(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	w := conn

	fmt.Fprintf(w, "220 fake.example.com ESMTP\r\n")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"):
			s.heloName = strings.TrimSpace(line[5:])
			fmt.Fprintf(w, "250-fake.example.com\r\n")
			if s.authAdvertised {
				fmt.Fprintf(w, "250 AUTH PLAIN\r\n")
			} else {
				fmt.Fprintf(w, "250 OK\r\n")
			}
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			s.authLine = line
			fmt.Fprintf(w, "235 Authentication successful\r\n")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			s.mailFrom = line
			fmt.Fprintf(w, "250 OK\r\n")
		case strings.HasPrefix(upper, "RCPT TO:"):
			s.rcptTo = line
			fmt.Fprintf(w, "250 OK\r\n")
		case upper == "DATA":
			fmt.Fprintf(w, "354 Start mail input; end with <CRLF>.<CRLF>\r\n")
			var body strings.Builder
			for {
				dataLine, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				body.WriteString(dataLine)
			}
			s.data = body.String()
			fmt.Fprintf(w, "250 OK: message accepted\r\n")
		case upper == "QUIT":
			fmt.Fprintf(w, "221 Bye\r\n")
			return
		default:
			fmt.Fprintf(w, "500 unrecognized command\r\n")
		}
	}
}

func TestSMTPEmailSender_SendsAPlainMessageWithNoAuth(t *testing.T) {
	addr, server := startFakeSMTPServer(t, false)
	host, port, _ := net.SplitHostPort(addr)

	sender := &SMTPEmailSender{Host: host, Port: port, From: "devplatform@example.com"}
	err := sender.Send("dev-1@example.com", "DevPlatform bildirimi", "Görev size atandı")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if !strings.Contains(server.mailFrom, "devplatform@example.com") {
		t.Errorf("MAIL FROM = %q, missing the from address", server.mailFrom)
	}
	if !strings.Contains(server.rcptTo, "dev-1@example.com") {
		t.Errorf("RCPT TO = %q, missing the recipient", server.rcptTo)
	}
	if !strings.Contains(server.data, "Görev size atandı") {
		t.Errorf("DATA = %q, missing the body", server.data)
	}
	if !strings.Contains(server.data, "Subject: DevPlatform bildirimi") {
		t.Errorf("DATA = %q, missing the subject header", server.data)
	}
	if server.authLine != "" {
		t.Errorf("AUTH was attempted (%q) against a server with no Username configured", server.authLine)
	}
}

func TestSMTPEmailSender_AuthenticatesWhenUsernameIsSetAndServerSupportsIt(t *testing.T) {
	addr, server := startFakeSMTPServer(t, true)
	host, port, _ := net.SplitHostPort(addr)

	sender := &SMTPEmailSender{
		Host: host, Port: port,
		Username: "devplatform", Password: "s3cret",
		From: "devplatform@example.com",
	}
	if err := sender.Send("dev-1@example.com", "subj", "body"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if server.authLine == "" {
		t.Fatal("expected the client to attempt AUTH against a server that advertised it")
	}
	if !strings.HasPrefix(server.authLine, "AUTH PLAIN ") {
		t.Errorf("authLine = %q, want an \"AUTH PLAIN <base64>\" line", server.authLine)
	}
}

func TestSMTPEmailSender_SkipsAuthWhenServerDoesNotAdvertiseIt(t *testing.T) {
	addr, server := startFakeSMTPServer(t, false)
	host, port, _ := net.SplitHostPort(addr)

	// Username is set, but the server (like a plain internal relay) never
	// offers AUTH — attempting it anyway would just break the exchange.
	sender := &SMTPEmailSender{
		Host: host, Port: port,
		Username: "devplatform", Password: "s3cret",
		From: "devplatform@example.com",
	}
	if err := sender.Send("dev-1@example.com", "subj", "body"); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if server.authLine != "" {
		t.Errorf("AUTH was attempted (%q) against a server that never advertised it", server.authLine)
	}
}

func TestSMTPEmailSender_WrapsDialErrorForAnUnreachableServer(t *testing.T) {
	// Port 1 is reserved and nothing listens there — Dial fails immediately
	// rather than hanging, so this test needs no timeout handling of its own.
	sender := &SMTPEmailSender{Host: "127.0.0.1", Port: "1", From: "devplatform@example.com"}

	err := sender.Send("dev-1@example.com", "subj", "body")
	if err == nil {
		t.Fatal("expected an error dialing an unreachable server")
	}
}
