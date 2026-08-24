package db

import (
	"encoding/base64"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestDB_WebAuthnCredentials_Lifecycle(t *testing.T) {
	d := newTestDB(t)

	uID := seedUser(t, d, "passkey_user@example.com")

	// 1. Initial list -> empty
	creds, err := d.GetWebAuthnCredentials(uID)
	if err != nil {
		t.Fatalf("GetWebAuthnCredentials error: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("expected 0 credentials initially, got %d", len(creds))
	}

	// 2. Create credential
	dummyCred := &webauthn.Credential{
		ID:              []byte("cred-1234567890"),
		PublicKey:       []byte("pubkey-bytes"),
		AttestationType: "none",
		Authenticator: webauthn.Authenticator{
			AAGUID:    []byte("00000000-0000-0000-0000-000000000000"),
			SignCount: 0,
		},
	}
	if err := d.CreateWebAuthnCredential(uID, "My MacBook Passkey", dummyCred); err != nil {
		t.Fatalf("CreateWebAuthnCredential error: %v", err)
	}

	// 3. GetWebAuthnCredentials -> 1 item
	creds, err = d.GetWebAuthnCredentials(uID)
	if err != nil {
		t.Fatalf("GetWebAuthnCredentials error: %v", err)
	}
	if len(creds) != 1 || string(creds[0].ID) != "cred-1234567890" {
		t.Fatalf("expected 1 credential matching ID, got %+v", creds)
	}

	// 4. GetWebAuthnUserIDByCredentialID
	foundUID, err := d.GetWebAuthnUserIDByCredentialID([]byte("cred-1234567890"))
	if err != nil || foundUID != uID {
		t.Fatalf("GetWebAuthnUserIDByCredentialID: expected user %d, got %d (err: %v)", uID, foundUID, err)
	}
	_, err = d.GetWebAuthnUserIDByCredentialID([]byte("non-existent-cred"))
	if err == nil {
		t.Fatalf("expected error for non-existent credential ID")
	}

	// 5. ListWebAuthnCredentials
	list, err := d.ListWebAuthnCredentials(uID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListWebAuthnCredentials error: %v, count: %d", err, len(list))
	}
	if list[0].Name != "My MacBook Passkey" || list[0].LastUsedAt != nil {
		t.Fatalf("unexpected credential display info: %+v", list[0])
	}

	// 6. UpdateWebAuthnCredentialUsed
	if err := d.UpdateWebAuthnCredentialUsed([]byte("cred-1234567890"), 5); err != nil {
		t.Fatalf("UpdateWebAuthnCredentialUsed error: %v", err)
	}
	listAfterUse, err := d.ListWebAuthnCredentials(uID)
	if err != nil || len(listAfterUse) != 1 || listAfterUse[0].LastUsedAt == nil {
		t.Fatalf("expected LastUsedAt non-nil after use: %+v", listAfterUse)
	}

	// 7. DeleteWebAuthnCredential
	idB64 := base64.RawURLEncoding.EncodeToString([]byte("cred-1234567890"))
	if err := d.DeleteWebAuthnCredential(idB64, uID); err != nil {
		t.Fatalf("DeleteWebAuthnCredential error: %v", err)
	}
	listAfterDel, err := d.ListWebAuthnCredentials(uID)
	if err != nil || len(listAfterDel) != 0 {
		t.Fatalf("expected 0 credentials after delete, got %d", len(listAfterDel))
	}
}
