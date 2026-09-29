package auth

import (
	"strings"
	"testing"
	"time"
)

func TestToken(t *testing.T) {
	s := New(strings.Repeat("a", 32))
	now := time.Now()
	token := s.Mint(42, now.Add(time.Minute))
	id, err := s.Verify(token, now)
	if err != nil || id != 42 {
		t.Fatal(id, err)
	}
	for _, bad := range []string{strings.Replace(token, "42.", "43.", 1), token + "x", "", s.Mint(-1, now.Add(time.Minute)), s.Mint(42, now)} {
		if _, err := s.Verify(bad, now); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := New(strings.Repeat("b", 32)).Verify(token, now); err == nil {
		t.Fatal("wrong secret accepted")
	}
}
