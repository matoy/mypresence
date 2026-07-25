package db

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnCredentialInfo holds display metadata for a passkey credential.
type WebAuthnCredentialInfo struct {
	IDBase64   string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// GetWebAuthnCredentials returns all WebAuthn credentials for a user,
// deserialized into the webauthn.Credential type expected by the library.
func (d *DB) GetWebAuthnCredentials(userID int64) ([]webauthn.Credential, error) {
	rows, err := d.core.Query(
		`SELECT credential_json FROM webauthn_credentials WHERE user_id = ?`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var creds []webauthn.Credential
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cred webauthn.Credential
		if err := json.Unmarshal([]byte(raw), &cred); err != nil {
			return nil, err
		}
		creds = append(creds, cred)
	}
	return creds, rows.Err()
}

// GetWebAuthnUserIDByCredentialID returns the user ID associated with a raw credential ID.
// Used during discoverable login to look up which user is authenticating.
func (d *DB) GetWebAuthnUserIDByCredentialID(credID []byte) (int64, error) {
	idB64 := base64.RawURLEncoding.EncodeToString(credID)
	var userID int64
	err := d.core.QueryRow(
		`SELECT user_id FROM webauthn_credentials WHERE id = ?`,
		idB64,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, sql.ErrNoRows
	}
	return userID, err
}

// CreateWebAuthnCredential persists a new passkey credential for a user.
func (d *DB) CreateWebAuthnCredential(userID int64, name string, cred *webauthn.Credential) error {
	idB64 := base64.RawURLEncoding.EncodeToString(cred.ID)
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	_, err = d.core.Exec(
		d.dialect.rebind(`INSERT INTO webauthn_credentials (id, user_id, name, credential_json, created_at) VALUES (?, ?, ?, ?, ?)`),
		idB64, userID, name, string(raw), time.Now(),
	)
	return err
}

// UpdateWebAuthnCredentialUsed updates the sign count and last_used_at for a credential
// after a successful authentication.
func (d *DB) UpdateWebAuthnCredentialUsed(credID []byte, signCount uint32) error {
	idB64 := base64.RawURLEncoding.EncodeToString(credID)

	// Re-serialize the updated credential by loading and patching it.
	var raw string
	err := d.core.QueryRow(
		`SELECT credential_json FROM webauthn_credentials WHERE id = ?`,
		idB64,
	).Scan(&raw)
	if err != nil {
		return err
	}
	var cred webauthn.Credential
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		return err
	}
	cred.Authenticator.SignCount = signCount
	updated, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	_, err = d.core.Exec(
		d.dialect.rebind(`UPDATE webauthn_credentials SET credential_json = ?, last_used_at = ? WHERE id = ?`),
		string(updated), time.Now(), idB64,
	)
	return err
}

// ListWebAuthnCredentials returns display-friendly credential info for a user's settings page.
func (d *DB) ListWebAuthnCredentials(userID int64) ([]WebAuthnCredentialInfo, error) {
	rows, err := d.core.Query(
		d.dialect.rebind(`SELECT id, name, created_at, last_used_at FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at ASC`),
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var list []WebAuthnCredentialInfo
	for rows.Next() {
		var info WebAuthnCredentialInfo
		var lastUsed sql.NullTime
		if err := rows.Scan(&info.IDBase64, &info.Name, &info.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			info.LastUsedAt = &t
		}
		list = append(list, info)
	}
	return list, rows.Err()
}

// DeleteWebAuthnCredential removes a passkey credential, scoped to the owning user.
func (d *DB) DeleteWebAuthnCredential(idBase64 string, userID int64) error {
	_, err := d.core.Exec(
		d.dialect.rebind(`DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`),
		idBase64, userID,
	)
	return err
}
