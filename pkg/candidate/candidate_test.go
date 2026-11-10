package candidate

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	priv := []string{"10.0.0.1", "192.168.1.2", "172.16.5.4", "127.0.0.1", "169.254.1.1", "fc00::1", "::1"}
	pub := []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}
	for _, s := range priv {
		if !IsPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s should be private", s)
		}
	}
	for _, s := range pub {
		if IsPrivateIP(net.ParseIP(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}

func TestPublicOnlyStripsPrivate(t *testing.T) {
	in := []Candidate{
		New(TypeHost, "192.168.1.5", 1000),
		New(TypeSrflx, "203.0.113.9", 2000),
		New(TypeHost, "10.0.0.2", 1000),
	}
	out := PublicOnly(in)
	if len(out) != 1 || out[0].IP != "203.0.113.9" || out[0].Port != 2000 {
		t.Fatalf("unexpected public set: %+v", out)
	}
}

func TestGatherHostIncludesLocal(t *testing.T) {
	cs := GatherHost(5555)
	if len(cs) == 0 {
		t.Fatal("expected at least one host candidate")
	}
	for _, c := range cs {
		if c.Port != 5555 {
			t.Fatalf("port mismatch: %v", c)
		}
		if c.Type != TypeHost {
			t.Fatalf("type %s", c.Type)
		}
	}
}

func TestMergeDedup(t *testing.T) {
	a := []Candidate{New(TypeHost, "127.0.0.1", 1)}
	b := []Candidate{New(TypeHost, "127.0.0.1", 1), New(TypeSrflx, "8.8.8.8", 2)}
	m := Merge(a, b)
	if len(m) != 2 {
		t.Fatalf("got %d", len(m))
	}
}
