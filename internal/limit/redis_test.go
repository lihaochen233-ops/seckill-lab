package limit

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAtomicWindowAndExpiry(t *testing.T) {
	s := miniredis.RunT(t)
	r := NewRedis(s.Addr(), "", 10)
	defer r.Close()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := r.Allow(context.Background())
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatal(accepted.Load())
	}
	if ttl := s.TTL("seckill-lab:orders:rate:v1"); ttl != time.Second {
		t.Fatal(ttl)
	}
	s.FastForward(time.Second)
	if ok, err := r.Allow(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	s.Close()
	if _, err := r.Allow(context.Background()); err == nil {
		t.Fatal("redis unavailable should return error")
	}
}
