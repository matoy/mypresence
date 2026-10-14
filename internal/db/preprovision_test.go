package db_test

import (
	"testing"

	"github.com/matoy/mypresence/internal/config"
	"github.com/matoy/mypresence/internal/db"
	"github.com/matoy/mypresence/internal/models"
)

func TestCleanNameFromEmail(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"jean-pierre.dupont@company.com", "Jean-Pierre Dupont"},
		{"alice_smith@domain.org", "Alice Smith"},
		{"bob@example.com", "Bob"},
		{"marie-claire_de-la-tour@corp.fr", "Marie-Claire De-La-Tour"},
		{"invalid", "Invalid"},
		{"", ""},
	}

	for _, c := range cases {
		got := db.CleanNameFromEmail(c.input)
		if got != c.expected {
			t.Errorf("CleanNameFromEmail(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestPreprovisionUserAndLastLogin(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(&config.Config{
		DBDriver: "sqlite",
		DataDir:  dir,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// Error on empty email
	_, err = d.PreprovisionUser("", "Test", 0)
	if err == nil {
		t.Fatalf("expected error on empty email")
	}

	// Create a site
	siteID, err := d.CreateSite(models.Site{
		Name:        "Headquarters",
		CountryCode: "FR",
		Seats:       50,
	})
	if err != nil {
		t.Fatalf("CreateSite failed: %v", err)
	}

	// Preprovision user with automatic name deduction and site
	uid, err := d.PreprovisionUser("sophie.martin@example.com", "", siteID)
	if err != nil {
		t.Fatalf("PreprovisionUser failed: %v", err)
	}

	u, err := d.GetUserByID(uid)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if u.Email != "sophie.martin@example.com" {
		t.Errorf("got email %q; want sophie.martin@example.com", u.Email)
	}
	if u.Name != "Sophie Martin" {
		t.Errorf("got name %q; want 'Sophie Martin'", u.Name)
	}
	if u.SiteID != siteID {
		t.Errorf("got siteID %d; want %d", u.SiteID, siteID)
	}
	if u.IsLocal {
		t.Errorf("expected IsLocal=false for preprovisioned user")
	}
	if u.LastLoginAt != nil {
		t.Errorf("expected LastLoginAt to be nil, got %v", u.LastLoginAt)
	}

	// Verify GetUsersBySite also has LastLoginAt nil
	siteUsers, err := d.GetUsersBySite(siteID)
	if err != nil {
		t.Fatalf("GetUsersBySite failed: %v", err)
	}
	if len(siteUsers) != 1 || siteUsers[0].LastLoginAt != nil {
		t.Errorf("GetUsersBySite: expected 1 user with nil LastLoginAt")
	}

	// Simulate user login via CreateSession
	tok, err := d.CreateSession(uid)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if tok == "" {
		t.Fatalf("expected non-empty session token")
	}

	// Check that LastLoginAt is now populated
	uAfterLogin, err := d.GetUserByEmail("sophie.martin@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if uAfterLogin.LastLoginAt == nil {
		t.Fatalf("expected LastLoginAt to be populated after session creation")
	}

	// Verify ListUsers includes LastLoginAt
	allUsers, err := d.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	found := false
	for _, usr := range allUsers {
		if usr.ID == uid {
			found = true
			if usr.LastLoginAt == nil {
				t.Errorf("ListUsers: user LastLoginAt is nil")
			}
		}
	}
	if !found {
		t.Errorf("user not found in ListUsers")
	}
}
