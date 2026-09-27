package core

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tunnelchat/pkg/config"
)

func TestTwoPeersChat(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create Alice
	aliceCfgPath := filepath.Join(tmpDir, "alice.xml")
	aliceCfg, err := config.LoadConfig(aliceCfgPath)
	if err != nil {
		t.Fatalf("load alice config: %v", err)
	}
	aliceCfg.User.Port = 58111
	aliceCfg.User.TryPort = 48111
	aliceCfg.User.IP = "127.0.0.1"

	aliceEngine, err := NewEngine(aliceCfg, aliceCfgPath)
	if err != nil {
		t.Fatalf("new alice engine: %v", err)
	}
	aliceEngine.CreateUser("Alice", "alicepass")
	if err := aliceEngine.Start(); err != nil {
		t.Fatalf("start alice: %v", err)
	}
	defer aliceEngine.Stop()

	// 2. Create Bob
	bobCfgPath := filepath.Join(tmpDir, "bob.xml")
	bobCfg, err := config.LoadConfig(bobCfgPath)
	if err != nil {
		t.Fatalf("load bob config: %v", err)
	}
	bobCfg.User.Port = 58222
	bobCfg.User.TryPort = 48222
	bobCfg.User.IP = "127.0.0.1"

	bobEngine, err := NewEngine(bobCfg, bobCfgPath)
	if err != nil {
		t.Fatalf("new bob engine: %v", err)
	}
	bobEngine.CreateUser("Bob", "bobpass")
	if err := bobEngine.Start(); err != nil {
		t.Fatalf("start bob: %v", err)
	}
	defer bobEngine.Stop()

	// Capture messages on Bob and Alice
	var wg sync.WaitGroup
	wg.Add(2)

	var bobRecvMsg string
	var aliceRecvMsg string

	bobEngine.SetChatCallback(func(friend config.Friend, text string) {
		t.Logf("Bob received from %s: %s", friend.Name, text)
		bobRecvMsg = text
		wg.Done()
	})

	aliceEngine.SetChatCallback(func(friend config.Friend, text string) {
		t.Logf("Alice received from %s: %s", friend.Name, text)
		aliceRecvMsg = text
		wg.Done()
	})

	// Alice adds Bob
	bobInfo := bobEngine.GetInfoString()
	aliceInfo := aliceEngine.GetInfoString()

	if err := aliceEngine.AddFriendByInfo(bobInfo); err != nil {
		t.Fatalf("Alice add Bob failed: %v", err)
	}

	// Bob adds Alice
	if err := bobEngine.AddFriendByInfo(aliceInfo); err != nil {
		t.Fatalf("Bob add Alice failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Alice sends message to Bob
	if err := aliceEngine.SendChatMessage("Bob", "Hello Bob, this is Alice!"); err != nil {
		t.Fatalf("Alice send message to Bob failed: %v", err)
	}

	// Bob replies to Alice
	if err := bobEngine.SendChatMessage("Alice", "Hi Alice, good to chat!"); err != nil {
		t.Fatalf("Bob send message to Alice failed: %v", err)
	}

	// Wait for both messages to be received
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for messages")
	}

	if bobRecvMsg != "Hello Bob, this is Alice!" {
		t.Errorf("Bob received unexpected message: %s", bobRecvMsg)
	}
	if aliceRecvMsg != "Hi Alice, good to chat!" {
		t.Errorf("Alice received unexpected message: %s", aliceRecvMsg)
	}
}
