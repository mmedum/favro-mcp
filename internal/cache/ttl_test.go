package cache

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTTL_GetMiss_ReturnsZeroAndFalse(t *testing.T) {
	t.Parallel()

	var c TTL[string]
	v, ok := c.Get("nope")
	if ok {
		t.Error("ok = true, want false")
	}
	if len(v) != 0 {
		t.Errorf("v = %v, want empty", v)
	}
}

func TestTTL_SetAndGet(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("k", 42, time.Minute)

	v, ok := c.Get("k")
	if !ok {
		t.Error("ok = false, want true")
	}
	if got := v; got != 42 {
		t.Errorf("v = %v, want %v", got, 42)
	}
}

func TestTTL_ExpiredEntry_TreatedAsMiss_AndEvicted(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := atomic.Pointer[time.Time]{}
	clock.Store(&now)

	c := TTL[string]{Now: func() time.Time { return *clock.Load() }}
	c.Set("k", "v", 100*time.Millisecond)

	// Advance the clock past expiry.
	later := now.Add(time.Second)
	clock.Store(&later)

	v, ok := c.Get("k")
	if ok {
		t.Error("expired entry must read as a miss")
	}
	if len(v) != 0 {
		t.Errorf("v = %v, want empty", v)
	}
	if got := c.Len(); got != 0 {
		t.Errorf("Get must lazily evict the expired entry: got %v, want %v", got, 0)
	}
}

func TestTTL_ZeroTTL_NeverExpiresByTime(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := atomic.Pointer[time.Time]{}
	clock.Store(&now)
	c := TTL[string]{Now: func() time.Time { return *clock.Load() }}

	c.Set("k", "v", 0) // zero ttl => sticky

	far := now.Add(24 * time.Hour)
	clock.Store(&far)

	v, ok := c.Get("k")
	if !ok {
		t.Error("ttl<=0 must mean never expire")
	}
	if got := v; got != "v" {
		t.Errorf("v = %v, want %v", got, "v")
	}
}

func TestTTL_Invalidate(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)

	c.Invalidate("a")
	_, ok := c.Get("a")
	if ok {
		t.Error("ok = true, want false")
	}

	v, ok := c.Get("b")
	if !ok {
		t.Error("ok = false, want true")
	}
	if got := v; got != 2 {
		t.Errorf("v = %v, want %v", got, 2)
	}
}

func TestTTL_InvalidatePrefix(t *testing.T) {
	t.Parallel()

	var c TTL[string]
	c.Set("org-1:tags:x", "x", time.Minute)
	c.Set("org-1:tags:y", "y", time.Minute)
	c.Set("org-1:users:z", "z", time.Minute)
	c.Set("org-2:tags:q", "q", time.Minute)

	n := c.InvalidatePrefix("org-1:tags:")
	if got := n; got != 2 {
		t.Errorf("n = %v, want %v", got, 2)
	}

	_, ok := c.Get("org-1:tags:x")
	if ok {
		t.Error("ok = true, want false")
	}
	_, ok = c.Get("org-1:tags:y")
	if ok {
		t.Error("ok = true, want false")
	}
	_, ok = c.Get("org-1:users:z")
	if !ok {
		t.Error("non-matching keys must be untouched")
	}
	_, ok = c.Get("org-2:tags:q")
	if !ok {
		t.Error("different-org keys must be untouched")
	}
}

func TestTTL_InvalidatePrefix_EmptyPrefixClearsAll(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)

	n := c.InvalidatePrefix("")
	if got := n; got != 2 {
		t.Errorf("n = %v, want %v", got, 2)
	}
	if got := c.Len(); got != 0 {
		t.Errorf("c.Len() = %v, want %v", got, 0)
	}
}

func TestTTL_Clear(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Clear()
	if got := c.Len(); got != 0 {
		t.Errorf("c.Len() = %v, want %v", got, 0)
	}
}

func TestTTL_Sweep_RemovesOnlyExpired(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := atomic.Pointer[time.Time]{}
	clock.Store(&now)
	c := TTL[int]{Now: func() time.Time { return *clock.Load() }}

	c.Set("short", 1, 100*time.Millisecond)
	c.Set("long", 2, time.Hour)
	c.Set("sticky", 3, 0)

	later := now.Add(time.Second)
	clock.Store(&later)

	n := c.Sweep()
	if got := n; got != 1 {
		t.Errorf("only the short-TTL entry should sweep: got %v, want %v", got, 1)
	}
	if got := c.Len(); got != 2 {
		t.Errorf("c.Len() = %v, want %v", got, 2)
	}
}

func TestTTL_Concurrent_NoDataRace(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	const workers = 8
	const ops = 200

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				key := keyFor(id, j%4) // small key space so we exercise overlap
				c.Set(key, j, time.Minute)
				_, _ = c.Get(key)
				if j%10 == 0 {
					c.Invalidate(key)
				}
			}
		}(i)
	}
	wg.Wait()
	// We don't assert size — race detector covers correctness.
}

// keyFor builds a deterministic small key namespace.
func keyFor(worker, slot int) string {
	return "k:" + strconv.Itoa(worker) + ":" + strconv.Itoa(slot)
}
