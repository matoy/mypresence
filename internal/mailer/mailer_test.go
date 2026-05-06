package mailer

import (
	"strings"
	"testing"
)

func TestBuildMessageContainsHeadersAndBody(t *testing.T) {
	msg := string(buildMessage("from@example.com", "to@example.com", "Hello", "Body line"))

	for _, want := range []string{
		"From: from@example.com\r\n",
		"To: to@example.com\r\n",
		"Subject: Hello\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\nBody line",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q in %q", want, msg)
		}
	}
}

func TestSendInvalidURLReturnsError(t *testing.T) {
	err := Send("://bad-url", "from@example.com", "to@example.com", "s", "b")
	if err == nil {
		t.Fatal("expected invalid SMTP URL error")
	}
	if !strings.Contains(err.Error(), "invalid SMTP URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendTLSDialFailureReturnsError(t *testing.T) {
	err := sendTLS("127.0.0.1:1", "localhost", nil, "from@example.com", "to@example.com", []byte("x"))
	if err == nil {
		t.Fatal("expected TLS dial error")
	}
	if !strings.Contains(err.Error(), "TLS dial") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendSMTPWithAuthConnectionError(t *testing.T) {
	err := Send("smtp://user:pass@127.0.0.1:1", "from@example.com", "to@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestSendSMTPSWithAuthConnectionError(t *testing.T) {
	err := Send("smtps://user:pass@127.0.0.1:1", "from@example.com", "to@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected tls connection error")
	}
}
