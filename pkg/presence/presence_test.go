package presence

import (
	"testing"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/identity"
)

func TestPresenceRoundTripAndRejects(t *testing.T) {
	id, err := identity.Generate("Alice")
	if err != nil {
		t.Fatal(err)
	}
	cands := []candidate.Candidate{
		candidate.New(candidate.TypeHost, "192.168.1.9", 1111),
		candidate.New(candidate.TypeSrflx, "203.0.113.8", 2222),
	}
	pkt, err := Encode(id, cands)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Verify(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "Alice" || !rec.Public.Equal(id.Public()) {
		t.Fatalf("identity mismatch: %+v", rec)
	}
	if len(rec.Candidates) != 1 || rec.Candidates[0].IP != "203.0.113.8" {
		t.Fatalf("private candidates must be stripped, got %+v", rec.Candidates)
	}

	tampered := append([]byte(nil), pkt...)
	tampered[len(tampered)-1] ^= 0x11
	if _, err := Verify(tampered); err != ErrSignature {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestQueryPacket(t *testing.T) {
	q := EncodeQuery()
	if !IsQuery(q) || IsRecord(q) {
		t.Fatal("query classification")
	}
	if _, err := Verify(q); err == nil {
		t.Fatal("query must not verify as a presence record")
	}
}
