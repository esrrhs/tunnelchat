package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	xmlPath := filepath.Join(tmpDir, "tunnelchat.xml")

	// Load non-existing should return defaults
	cfg, err := LoadConfig(xmlPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.STUNServer.STUNs) == 0 {
		t.Fatal("expected default STUN servers, got 0")
	}

	cfg.SetUser(User{
		Acc:     "TEST_ACC_123",
		Name:    "Alice",
		IP:      "1.2.3.4",
		Port:    55555,
		TryPort: 44444,
		Pwd:     "PWD_HASH",
	})

	cfg.SetFriend(Friend{
		Acc:  "BOB_ACC_456",
		Name: "Bob",
		IP:   "5.6.7.8",
		Port: 66666,
		SKey: "SKEY1",
		RKey: "RKEY1",
	})

	if err := cfg.SaveConfig(xmlPath); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file on disk
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	t.Logf("Saved XML:\n%s", string(data))

	// Reload config
	cfg2, err := LoadConfig(xmlPath)
	if err != nil {
		t.Fatalf("Reload Config failed: %v", err)
	}

	u := cfg2.GetUser()
	if u.Acc != "TEST_ACC_123" || u.Name != "Alice" || u.Port != 55555 {
		t.Fatalf("User mismatch: %+v", u)
	}

	f, ok := cfg2.GetFriend("BOB_ACC_456")
	if !ok || f.Name != "Bob" || f.SKey != "SKEY1" {
		t.Fatalf("Friend mismatch: %+v", f)
	}

	fByName, ok := cfg2.GetFriendByName("Bob")
	if !ok || fByName.Acc != "BOB_ACC_456" {
		t.Fatalf("Friend by name mismatch: %+v", fByName)
	}
}
