package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"tunnelchat/pkg/identity"
)

func TestCreateLoadNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alice.json")

	cfg, err := CreateIdentity(path, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	id1, err := cfg.Identity()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", st.Mode().Perm())
	}

	if _, err = CreateIdentity(path, "Alice2"); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}

	cfg2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := cfg2.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if id1.Seed != id2.Seed || id2.Name != "Alice" {
		t.Fatal("identity was mutated")
	}
}

func TestLegacyXMLNotTouched(t *testing.T) {
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "tunnelchat.xml")
	legacy := []byte(`<?xml version="1.0"?>
<Config>
    <User acc="OLDACC" name="Alice" ip="1.2.3.4" port="55555" tryport="44444" pwd="PWD_HASH"></User>
    <FriendList>
        <Friend acc="BOB" name="Bob" ip="5.6.7.8" port="66666" skey="SKEY1" rkey="RKEY1"></Friend>
    </FriendList>
</Config>
`)
	if err := os.WriteFile(xmlPath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(xmlPath); !errors.Is(err, ErrLegacyConfig) {
		t.Fatalf("expected ErrLegacyConfig, got %v", err)
	}
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(legacy) {
		t.Fatal("legacy file was modified")
	}
	if _, err = CreateIdentity(xmlPath, "Alice"); !errors.Is(err, ErrLegacyConfig) {
		t.Fatalf("must not create over xml path, got %v", err)
	}
	data, _ = os.ReadFile(xmlPath)
	if string(data) != string(legacy) {
		t.Fatal("legacy file was modified by create")
	}
}

func TestAddFriendRejectsKeyChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	cfg, err := CreateIdentity(path, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := identity.Generate("Bob")
	if err != nil {
		t.Fatal(err)
	}
	eve, err := identity.Generate("Eve")
	if err != nil {
		t.Fatal(err)
	}
	rv := make([]byte, 32)
	rv[0] = 1
	pubB := bob.X25519Public()
	if err := cfg.AddFriend(Friend{Name: "Bob", Ed25519: []byte(bob.Ed25519Public()), X25519: pubB[:], Rendezvous: rv}); err != nil {
		t.Fatal(err)
	}
	pubE := eve.X25519Public()
	if err := cfg.AddFriend(Friend{Name: "Bob", Ed25519: []byte(eve.Ed25519Public()), X25519: pubE[:], Rendezvous: rv}); err == nil {
		t.Fatal("should refuse overwriting Bob's identity")
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Identity(); err == nil {
		t.Fatal("expected no identity")
	}
}
