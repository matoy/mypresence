package mailer

import (
	"crypto/tls"
	"net"
	"net/smtp"
	"testing"
)

// startFullFakeSMTPServer starts a TLS SMTP server that completes the SMTP
// handshake (EHLO/MAIL FROM/RCPT TO/DATA/QUIT) in memory.
// Returns the listening address and a stop function.
func startFullFakeSMTPServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	cert := selfSignedCert(t) // defined in mailer_tls_test.go
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	l, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go handleFakeSMTP(conn)
		}
	}()
	return l.Addr().String(), func() { l.Close() } //nolint:errcheck
}

// handleFakeSMTP implements a minimal SMTP server conversation.
func handleFakeSMTP(conn net.Conn) {
	defer conn.Close()                                             //nolint:errcheck
	writeLine := func(s string) { conn.Write([]byte(s + "\r\n")) } //nolint:errcheck
	readLine := func() string {
		buf := make([]byte, 512)
		n, _ := conn.Read(buf)
		return string(buf[:n])
	}
	writeLine("220 fake.smtp ESMTP")
	cmd := readLine()
	if len(cmd) >= 4 && cmd[:4] == "EHLO" {
		writeLine("250-fake.smtp")
		writeLine("250-AUTH PLAIN LOGIN")
		writeLine("250 OK")
	} else {
		writeLine("250 OK")
	}
	for {
		line := readLine()
		if len(line) == 0 {
			return
		}
		switch {
		case len(line) >= 4 && line[:4] == "AUTH":
			writeLine("235 Authentication successful")
		case len(line) >= 4 && line[:4] == "MAIL":
			writeLine("250 OK")
		case len(line) >= 4 && line[:4] == "RCPT":
			writeLine("250 OK")
		case len(line) >= 4 && line[:4] == "DATA":
			writeLine("354 Start input")
			// read until "."
			buf := make([]byte, 4096)
			conn.Read(buf) //nolint:errcheck
			writeLine("250 OK")
		case len(line) >= 4 && line[:4] == "QUIT":
			writeLine("221 Bye")
			return
		default:
			writeLine("500 Unknown")
		}
	}
}

// injectInsecureTLSConfig installs a test hook that disables cert verification.
// Returns a cleanup function that restores the original hook.
func injectInsecureTLSConfig(t *testing.T) func() {
	t.Helper()
	old := tlsConfigForAddr
	tlsConfigForAddr = func(_, host string) *tls.Config {
		return &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: true, //nolint:gosec
			MinVersion:         tls.VersionTLS12,
		}
	}
	return func() { tlsConfigForAddr = old }
}

func TestSendTLS_SuccessNoAuth(t *testing.T) {
	addr, stop := startFullFakeSMTPServer(t)
	defer stop()
	defer injectInsecureTLSConfig(t)()

	host, _, _ := net.SplitHostPort(addr)
	err := sendTLS(addr, host, nil, "from@test.com", "to@test.com", []byte("Subject: test\r\n\r\nbody\r\n.\r\n"))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestSendTLS_SuccessWithAuth(t *testing.T) {
	addr, stop := startFullFakeSMTPServer(t)
	defer stop()
	defer injectInsecureTLSConfig(t)()

	host, _, _ := net.SplitHostPort(addr)
	auth := smtp.PlainAuth("", "user", "pass", host)
	err := sendTLS(addr, host, auth, "from@test.com", "to@test.com", []byte("Subject: test\r\n\r\nbody\r\n.\r\n"))
	if err != nil {
		t.Fatalf("expected success with auth, got: %v", err)
	}
}
