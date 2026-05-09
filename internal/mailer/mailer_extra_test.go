package mailer

import (
	"strings"
	"testing"
)

func TestSend_SMTPSScheme_DialFails(t *testing.T) {
	// smtps:// scheme takes the TLS path (sendTLS)
	err := Send("smtps://user:pass@127.0.0.1:1", "from@example.com", "to@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected connection error for smtps")
	}
	if !strings.Contains(err.Error(), "TLS dial") {
		t.Fatalf("expected TLS dial error, got: %v", err)
	}
}

func TestSend_SMTPSScheme_DefaultPort(t *testing.T) {
	// smtps:// without explicit port should use 465
	err := Send("smtps://127.0.0.1", "from@example.com", "to@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected connection error")
	}
	// Should fail with TLS dial on port 465
	if !strings.Contains(err.Error(), "TLS dial") {
		t.Fatalf("expected TLS dial error, got: %v", err)
	}
}

func TestSend_SMTPScheme_DefaultPort(t *testing.T) {
	// smtp:// without explicit port defaults to 587 (STARTTLS)
	err := Send("smtp://127.0.0.1", "from@example.com", "to@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected connection error on port 587")
	}
}

func TestSend_SMTPScheme_NoAuth(t *testing.T) {
	// smtp:// without user info — no auth
	err := Send("smtp://127.0.0.1:1", "from@example.com", "to@example.com", "subject", "body")
	if err == nil {
		t.Fatal("expected connection error")
	}
}
