package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/crewjam/saml"
)

func TestGetAttributeValueAndValues(t *testing.T) {
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				{
					Name:   "email",
					Values: []saml.AttributeValue{{Value: "user@example.com"}},
				},
				{
					Name:   "groups",
					Values: []saml.AttributeValue{{Value: "g1"}, {Value: "g2"}},
				},
			},
		}},
	}

	if got := getAttributeValue(a, "email"); got != "user@example.com" {
		t.Fatalf("getAttributeValue(email) = %q", got)
	}
	if got := getAttributeValue(a, "missing"); got != "" {
		t.Fatalf("getAttributeValue(missing) = %q, want empty", got)
	}

	vals := getAttributeValues(a, "groups")
	if len(vals) != 2 || vals[0] != "g1" || vals[1] != "g2" {
		t.Fatalf("unexpected groups values: %#v", vals)
	}
	if got := getAttributeValues(a, "missing"); got != nil {
		t.Fatalf("expected nil for missing attribute, got %#v", got)
	}
}

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("expected certificate bytes")
	}
	if cert.PrivateKey == nil {
		t.Fatal("expected private key")
	}
}

func TestClientIPPrefersXForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.5" {
		t.Fatalf("clientIP with XFF = %q", got)
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	if got := clientIP(req2); got != "10.0.0.2:5678" {
		t.Fatalf("clientIP fallback = %q", got)
	}
}
