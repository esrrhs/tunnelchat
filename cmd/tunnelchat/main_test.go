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

	// 1. Build the binary
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary failed: %v, output: %s", err, string(out))
	}

	aliceXml := filepath.Join(tmpDir, "alice.xml")
	bobXml := filepath.Join(tmpDir, "bob.xml")

	// 2. Test tunnelchat new
	cmd = exec.Command(binPath, "-c", aliceXml, "new", "Alice", "pass123")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alice new failed: %v, out: %s", err, string(out))
	}
	if !strings.Contains(string(out), "User successfully created!") {
		t.Fatalf("expected user created output, got: %s", string(out))
	}

	cmd = exec.Command(binPath, "-c", bobXml, "new", "Bob", "pass456")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bob new failed: %v, out: %s", err, string(out))
	}

	// 3. Test tunnelchat info
	cmd = exec.Command(binPath, "-c", aliceXml, "info")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alice info failed: %v, out: %s", err, string(out))
	}
	aliceOutput := string(out)
	if !strings.Contains(aliceOutput, "My Information:") {
		t.Fatalf("expected info output, got: %s", aliceOutput)
	}

	// 4. Verify config files exist on disk
	if _, err := os.Stat(aliceXml); os.IsNotExist(err) {
		t.Fatalf("alice.xml was not created")
	}
	if _, err := os.Stat(bobXml); os.IsNotExist(err) {
		t.Fatalf("bob.xml was not created")
	}
}
