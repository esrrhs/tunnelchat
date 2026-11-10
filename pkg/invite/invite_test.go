package invite

import (
	"strings"
	"testing"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/identity"
)

func TestInviteRoundTripAndTamper(t *testing.T) {
	id, err := identity.Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	var rv [32]byte
	rv[0] = 7
	cands := []candidate.Candidate{
		candidate.New(candidate.TypeHost, "192.168.1.10", 50000),
		candidate.New(candidate.TypeSrflx, "203.0.113.4", 4000),
	}
	s, err := Encode(id, rv, cands)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "TC2INV1.") {
		t.Fatalf("prefix: %s", s)
	}
	got, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Alice" || !got.Public.Equal(id.Public()) {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.Rendezvous != rv {
		t.Fatal("rendezvous mismatch")
	}
	if len(got.Candidates) != 2 || got.Candidates[0].Private == false || got.Candidates[1].Private == true {
		t.Fatalf("candidates: %+v", got.Candidates)
	}

	// Tamper payload
	raw := []byte(s)
	raw[len(raw)/2] ^= 0x1
	if _, err := Decode(string(raw)); err == nil {
		t.Fatal("tampered invite must fail")
	}
	if _, err := Decode("98F8E4DED6E5EEF5"); err == nil {
		t.Fatal("legacy string must be rejected")
	}
}
