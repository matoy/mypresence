package handlers

import (
	"sync"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/models"
)

// -----------------------------------------------------------------------
// collectPendingSAMLIDs
// -----------------------------------------------------------------------

func TestCollectPendingSAMLIDs_ValidAndExpired(t *testing.T) {
	var m sync.Map
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Minute)

	m.Store("valid1", future)
	m.Store("valid2", future)
	m.Store("expired1", past)

	ids := collectPendingSAMLIDs(&m)

	// valid IDs should be returned
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["valid1"] || !found["valid2"] {
		t.Errorf("expected valid IDs in result, got %v", ids)
	}
	if found["expired1"] {
		t.Error("expired ID should not be in result")
	}

	// expired entry should be deleted from map
	if _, ok := m.Load("expired1"); ok {
		t.Error("expected expired1 to be deleted from sync.Map")
	}
}

func TestCollectPendingSAMLIDs_Empty(t *testing.T) {
	var m sync.Map
	ids := collectPendingSAMLIDs(&m)
	if len(ids) != 0 {
		t.Errorf("expected empty result for empty map, got %v", ids)
	}
}

// -----------------------------------------------------------------------
// extractSAMLEmail
// -----------------------------------------------------------------------

func samlAttr(name, value string) saml.Attribute {
	return saml.Attribute{
		Name:   name,
		Values: []saml.AttributeValue{{Value: value}},
	}
}

func TestExtractSAMLEmail_EmailClaim(t *testing.T) {
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				samlAttr("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress", "user@example.com"),
			},
		}},
	}
	if got := extractSAMLEmail(a); got != "user@example.com" {
		t.Errorf("expected user@example.com, got %q", got)
	}
}

func TestExtractSAMLEmail_NameClaim(t *testing.T) {
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				samlAttr("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name", "name@example.com"),
			},
		}},
	}
	if got := extractSAMLEmail(a); got != "name@example.com" {
		t.Errorf("expected name@example.com, got %q", got)
	}
}

func TestExtractSAMLEmail_NameID(t *testing.T) {
	a := &saml.Assertion{
		Subject: &saml.Subject{
			NameID: &saml.NameID{Value: "nameid@example.com"},
		},
	}
	if got := extractSAMLEmail(a); got != "nameid@example.com" {
		t.Errorf("expected nameid@example.com, got %q", got)
	}
}

func TestExtractSAMLEmail_Empty(t *testing.T) {
	a := &saml.Assertion{}
	if got := extractSAMLEmail(a); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// -----------------------------------------------------------------------
// extractSAMLDisplayName
// -----------------------------------------------------------------------

func TestExtractSAMLDisplayName_DisplayName(t *testing.T) {
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				samlAttr("http://schemas.microsoft.com/identity/claims/displayname", "Alice Smith"),
			},
		}},
	}
	if got := extractSAMLDisplayName(a, "alice@example.com"); got != "Alice Smith" {
		t.Errorf("expected Alice Smith, got %q", got)
	}
}

func TestExtractSAMLDisplayName_GivenSurname(t *testing.T) {
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				samlAttr("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname", "Bob"),
				samlAttr("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname", "Jones"),
			},
		}},
	}
	got := extractSAMLDisplayName(a, "bob@example.com")
	if got != "Bob Jones" {
		t.Errorf("expected 'Bob Jones', got %q", got)
	}
}

func TestExtractSAMLDisplayName_EmailFallback(t *testing.T) {
	a := &saml.Assertion{}
	got := extractSAMLDisplayName(a, "fallback@example.com")
	if got != "fallback@example.com" {
		t.Errorf("expected email fallback, got %q", got)
	}
}

func TestExtractSAMLDisplayName_Empty(t *testing.T) {
	a := &saml.Assertion{}
	got := extractSAMLDisplayName(a, "")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// -----------------------------------------------------------------------
// syncSAMLGroupRoles
// -----------------------------------------------------------------------

func TestSyncSAMLGroupRoles_NoGroupsConfigured_EarlyReturn(t *testing.T) {
	d := newExtraTestDB(t)
	uid := seedUserInHandlers(t, d, "samlsync_nogroups@test.com")
	user, _ := d.GetUserByID(uid)

	h := &AuthHandler{DB: d, Config: newSAMLConfig()}
	// Config has no group mappings → early return, no role change
	a := &saml.Assertion{}
	h.syncSAMLGroupRoles(user, a, user.Email)
	// No panic, no role change — verify user still has basic role
	u2, _ := d.GetUserByID(uid)
	if u2.HasRole(models.RoleGlobal) {
		t.Error("expected no global role after no-op sync")
	}
}

func TestSyncSAMLGroupRoles_WithGroupMapping(t *testing.T) {
	d := newExtraTestDB(t)
	uid := seedUserInHandlers(t, d, "samlsync_group@test.com")
	user, _ := d.GetUserByID(uid)

	cfg := newSAMLConfig()
	cfg.SAMLGroupGlobal = "admins"
	cfg.SAMLGroupsClaim = "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups"

	h := &AuthHandler{DB: d, Config: cfg}
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				{
					Name:   cfg.SAMLGroupsClaim,
					Values: []saml.AttributeValue{{Value: "admins"}},
				},
			},
		}},
	}
	h.syncSAMLGroupRoles(user, a, user.Email)
	u2, _ := d.GetUserByID(uid)
	if !u2.HasRole(models.RoleGlobal) {
		t.Error("expected global role after group sync with 'admins' group")
	}
}

func TestSyncSAMLGroupRoles_NotInAnyGroup_GetsBasic(t *testing.T) {
	d := newExtraTestDB(t)
	uid := seedUserInHandlers(t, d, "samlsync_nogroup@test.com")
	d.UpdateUserRoles(uid, string(models.RoleGlobal)) //nolint:errcheck
	user, _ := d.GetUserByID(uid)

	cfg := newSAMLConfig()
	cfg.SAMLGroupGlobal = "admins"
	cfg.SAMLGroupsClaim = "groups"

	h := &AuthHandler{DB: d, Config: cfg}
	// Assertion with no matching groups → should set role to "basic"
	a := &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{{
			Attributes: []saml.Attribute{
				{
					Name:   "groups",
					Values: []saml.AttributeValue{{Value: "users"}},
				},
			},
		}},
	}
	h.syncSAMLGroupRoles(user, a, user.Email)
	u2, _ := d.GetUserByID(uid)
	if u2.HasRole(models.RoleGlobal) {
		t.Error("expected role demoted to basic after not matching any group")
	}
}

// newSAMLConfig returns a minimal config with no group mappings.
func newSAMLConfig() *config.Config {
	return &config.Config{
		SAMLEnabled: true,
	}
}
