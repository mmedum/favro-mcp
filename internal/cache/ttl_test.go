package cache

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTTL_GetMiss_ReturnsZeroAndFalse(t *testing.T) {
	t.Parallel()

	var c TTL[string]
	v, ok := c.Get("nope")
	require.False(t, ok)
	require.Empty(t, v)
}

func TestTTL_SetAndGet(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("k", 42, time.Minute)

	v, ok := c.Get("k")
	require.True(t, ok)
	require.Equal(t, 42, v)
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
	require.False(t, ok, "expired entry must read as a miss")
	require.Empty(t, v)
	require.Equal(t, 0, c.Len(), "Get must lazily evict the expired entry")
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
	require.True(t, ok, "ttl<=0 must mean never expire")
	require.Equal(t, "v", v)
}

func TestTTL_Invalidate(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)

	c.Invalidate("a")
	_, ok := c.Get("a")
	require.False(t, ok)

	v, ok := c.Get("b")
	require.True(t, ok)
	require.Equal(t, 2, v)
}

func TestTTL_InvalidatePrefix(t *testing.T) {
	t.Parallel()

	var c TTL[string]
	c.Set("org-1:tags:x", "x", time.Minute)
	c.Set("org-1:tags:y", "y", time.Minute)
	c.Set("org-1:users:z", "z", time.Minute)
	c.Set("org-2:tags:q", "q", time.Minute)

	n := c.InvalidatePrefix("org-1:tags:")
	require.Equal(t, 2, n)

	_, ok := c.Get("org-1:tags:x")
	require.False(t, ok)
	_, ok = c.Get("org-1:tags:y")
	require.False(t, ok)
	_, ok = c.Get("org-1:users:z")
	require.True(t, ok, "non-matching keys must be untouched")
	_, ok = c.Get("org-2:tags:q")
	require.True(t, ok, "different-org keys must be untouched")
}

func TestTTL_InvalidatePrefix_EmptyPrefixClearsAll(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)

	n := c.InvalidatePrefix("")
	require.Equal(t, 2, n)
	require.Equal(t, 0, c.Len())
}

func TestTTL_Clear(t *testing.T) {
	t.Parallel()

	var c TTL[int]
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Clear()
	require.Equal(t, 0, c.Len())
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
	require.Equal(t, 1, n, "only the short-TTL entry should sweep")
	require.Equal(t, 2, c.Len())
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
