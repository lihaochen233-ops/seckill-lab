package domain

import (
	"errors"
	"testing"
	"time"
)

func TestSaleBoundaries(t *testing.T) {
	now := time.Now()
	p := Product{Stock: 1, StartsAt: now, EndsAt: now.Add(time.Second)}
	if err := CheckSale(p, now); err != nil {
		t.Fatal(err)
	}
	if err := CheckSale(p, p.EndsAt); !errors.Is(err, ErrEnded) {
		t.Fatal(err)
	}
}
