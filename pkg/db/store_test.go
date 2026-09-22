package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemIntrospectionStore(t *testing.T) {
	// 1. Create a temporary SQLite database file for testing
	tmpDir, err := os.MkdirTemp("", "sysagent_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_agent.db")

	// 2. Initialize the store (this triggers our migration schema)
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// 3. Test System Profile Operations
	t.Run("SystemProfile", func(t *testing.T) {
		err := store.SetSystemProfile("hardware", "gpu", "NVIDIA GTX 1080")
		if err != nil {
			t.Errorf("failed to set system profile: %v", err)
		}

		// Test Upsert / Update capability
		err = store.SetSystemProfile("hardware", "gpu", "NVIDIA GTX 1080 (Overclocked)")
		if err != nil {
			t.Errorf("failed to update system profile: %v", err)
		}

		profile, err := store.GetSystemProfile()
		if err != nil {
			t.Fatalf("failed to get system profile: %v", err)
		}

		if len(profile) != 1 {
			t.Fatalf("expected 1 profile entry, got %d", len(profile))
		}

		if profile[0].Attribute != "gpu" || profile[0].Value != "NVIDIA GTX 1080 (Overclocked)" {
			t.Errorf("unexpected profile data: %+v", profile[0])
		}
	})

	// 4. Test System Quirks Operations
	t.Run("SystemQuirks", func(t *testing.T) {
		err := store.LogSystemQuirk("auth_logs", "Host uses systemd-journald instead of /var/log/auth.log")
		if err != nil {
			t.Errorf("failed to log quirk: %v", err)
		}

		quirks, err := store.GetSystemQuirks()
		if err != nil {
			t.Fatalf("failed to get quirks: %v", err)
		}

		if len(quirks) != 1 {
			t.Fatalf("expected 1 quirk, got %d", len(quirks))
		}

		quirkID := quirks[0].ID
		expectedDesc := "Host uses systemd-journald instead of /var/log/auth.log"
		if quirks[0].Description != expectedDesc {
			t.Errorf("unexpected quirk description: %s", quirks[0].Description)
		}

		// Test CorrectQuirk (Self-Correction)
		correctedDesc := "Host strictly requires journalctl for auth diagnostics."
		err = store.CorrectQuirk(quirkID, correctedDesc)
		if err != nil {
			t.Errorf("failed to correct quirk: %v", err)
		}

		quirksUpdated, err := store.GetSystemQuirks()
		if err != nil {
			t.Fatalf("failed to get updated quirks: %v", err)
		}

		if quirksUpdated[0].Description != correctedDesc {
			t.Errorf("quirk correction failed, got: %s", quirksUpdated[0].Description)
		}
	})
}

func TestSystemProfileSeeding(t *testing.T) {
	// 1. Initialize an in-memory SQLite store for testing
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer store.Close()

	// 2. Run the seed function / set a profile attribute
	err = store.SetSystemProfile("audio", "backend", "PipeWire")
	if err != nil {
		t.Fatalf("failed to set system profile: %v", err)
	}

	// 3. Retrieve the profile to verify it was saved correctly
	profiles, err := store.GetSystemProfile()
	if err != nil {
		t.Fatalf("failed to get system profile: %v", err)
	}

	// 4. Assert the results
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile entry, got %d", len(profiles))
	}

	p := profiles[0]
	if p.Category != "audio" || p.Attribute != "backend" || p.Value != "PipeWire" {
		t.Errorf("unexpected profile data retrieved: %+v", p)
	}
}
