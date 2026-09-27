package crypto

import (
	"testing"
)

func TestDESCompatibilityWithCpp(t *testing.T) {
	key := "fakechat"
	text := "598823FC359BBB43E556CEE9408AAA3C 124.127.214.52 54394"
	expectedEnc := "ABF50F976CD38C36383C8C91348D028222C018A94519700E413100C703CC80653CB229B346F95F70115059BD37C1E21A5C6E166ECD31E8F7"

	enc := DESEncrypt(key, text)
	if enc != expectedEnc {
		t.Fatalf("DESEncrypt output mismatch:\nExpected: %s\nActual:   %s", expectedEnc, enc)
	}

	dec := DESDecrypt(key, enc)
	if dec != text {
		t.Fatalf("DESDecrypt output mismatch:\nExpected: %s\nActual:   %s", text, dec)
	}
}

func TestDESRoundTrip(t *testing.T) {
	testCases := []struct {
		key  string
		text string
	}{
		{"chat", "alice hello world"},
		{"add", "ACC1 NAME1 KEY1 ACC2"},
		{"sync", "192.168.1.100 54321 bob"},
		{"short", "a"},
		{"exact8", "12345678"},
		{"exact16", "12345678abcdefgh"},
		{"chinese", "你好，世界！这是一条测试信息。"},
	}

	for _, tc := range testCases {
		enc := DESEncrypt(tc.key, tc.text)
		dec := DESDecrypt(tc.key, enc)
		if dec != tc.text {
			t.Errorf("Round trip failed for key=%q, text=%q. Got %q", tc.key, tc.text, dec)
		}
	}
}

func TestNewGUID(t *testing.T) {
	guid1 := NewGUID("alice")
	guid2 := NewGUID("alice")
	if len(guid1) != 32 {
		t.Errorf("expected 32 hex chars, got %d (%s)", len(guid1), guid1)
	}
	if guid1 == guid2 {
		t.Errorf("expected unique guids, got duplicates: %s", guid1)
	}
}
