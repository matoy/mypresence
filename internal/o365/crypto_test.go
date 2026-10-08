package o365

import (
	"testing"
)

func TestEncryptDecryptToken(t *testing.T) {
	secret := "test-secret-key-with-sufficient-length-32-chars"
	token := "ya29.a0AfH6SMD...some-access-token-1234567890"

	enc, err := EncryptToken(token, secret)
	if err != nil {
		t.Fatalf("EncryptToken failed: %v", err)
	}
	if enc == token {
		t.Fatal("Ciphertext should not match plaintext")
	}

	dec, err := DecryptToken(enc, secret)
	if err != nil {
		t.Fatalf("DecryptToken failed: %v", err)
	}
	if dec != token {
		t.Fatalf("Expected %s, got %s", token, dec)
	}

	// Tampered secret
	_, err = DecryptToken(enc, "wrong-secret-key")
	if err == nil {
		t.Fatal("Expected error with wrong secret key")
	}

	// Empty string
	encEmpty, err := EncryptToken("", secret)
	if err != nil || encEmpty != "" {
		t.Fatalf("Expected empty string, got %s, err %v", encEmpty, err)
	}
	decEmpty, err := DecryptToken("", secret)
	if err != nil || decEmpty != "" {
		t.Fatalf("Expected empty string, got %s, err %v", decEmpty, err)
	}
}
