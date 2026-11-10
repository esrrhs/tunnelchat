package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLINewInfoAdd(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "tunnelchat")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary failed: %v, output: %s", err, string(out))
	}

	alice := filepath.Join(tmpDir, "alice.json")
	bob := filepath.Join(tmpDir, "bob.json")

	cmd = exec.Command(binPath, "-c", alice, "new", "Alice", "pass123")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alice new failed: %v, out: %s", err, string(out))
	}
	if !strings.Contains(string(out), "User successfully created!") {
		t.Fatalf("expected user created output, got: %s", string(out))
	}
	if !strings.Contains(string(out), "password argument is ignored") {
		t.Fatalf("expected password-ignored warning, got: %s", string(out))
	}

	cmd = exec.Command(binPath, "-c", alice, "new", "Alice2")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("overwrite should fail, out: %s", out)
	}

	cmd = exec.Command(binPath, "-c", bob, "new", "Bob")
	if out, err = cmd.CombinedOutput(); err != nil {
		t.Fatalf("bob new failed: %v, out: %s", err, string(out))
	}

	cmd = exec.Command(binPath, "-c", alice, "--no-stun", "info")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alice info failed: %v, out: %s", err, string(out))
	}
	aliceOutput := string(out)
	if !strings.Contains(aliceOutput, "My Information:") || !strings.Contains(aliceOutput, "TC2INV1.") {
		t.Fatalf("expected v2 info output, got: %s", aliceOutput)
	}

	inv := extractInvite(aliceOutput)
	if inv == "" {
		t.Fatalf("no invite in info output: %s", aliceOutput)
	}
	cmd = exec.Command(binPath, "-c", bob, "add", inv)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bob add failed: %v, out: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Friend added successfully") {
		t.Fatalf("add output: %s", out)
	}

	if _, err := os.Stat(alice); os.IsNotExist(err) {
		t.Fatal("alice.json missing")
	}
	// Legacy xml path must be rejected and not created if we try.
	xmlPath := filepath.Join(tmpDir, "old.xml")
	cmd = exec.Command(binPath, "-c", xmlPath, "new", "Zed")
	if out, err = cmd.CombinedOutput(); err == nil {
		t.Fatalf("xml path should be rejected, out=%s", out)
	}
}

func TestRunHelp(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Fatalf("help exit %d", code)
	}
}

func extractInvite(s string) string {
	i := strings.Index(s, "TC2INV1.")
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.IndexByte(rest, 0x1b); j > 0 {
		rest = rest[:j]
	}
	if j := strings.IndexAny(rest, "\n\r\t "); j > 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}
