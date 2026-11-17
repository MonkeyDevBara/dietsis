// Package limit implements an in-memory token-bucket rate limiter keyed by string (IP or user id).
// State is per process and lost on restart, which is acceptable for a single-instance service.
package limit

import (
	"math"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter allows `burst` events immediately and refills at `perMinute` per minute.
type Limiter struct {
	rate     float64 // tokens per second
	burst    float64
	maxKeys  int
	now      func() time.Time
	mu       sync.Mutex
	buckets  map[string]*bucket
	lastScan time.Time
}

// New creates a limiter. maxKeys bounds memory: past it, idle buckets are swept, and if the table is
// still full new keys share a single overflow bucket instead of growing without limit.
func New(perMinute float64, burst int) *Limiter {
	return newWithClock(perMinute, burst, 100_000, time.Now)
}

func newWithClock(perMinute float64, burst, maxKeys int, now func() time.Time) *Limiter {
	return &Limiter{
		rate:    perMinute / 60,
		burst:   float64(burst),
		maxKeys: maxKeys,
		now:     now,
		buckets: make(map[string]*bucket),
	}
}

const overflowKey = "\x00overflow"

// Allow consumes one token for key. When denied, retryAfter is how long until one token is available.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)
	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= l.maxKeys {
			key = overflowKey
			b = l.buckets[key]
		}
		if b == nil {
			b = &bucket{tokens: l.burst, last: now}
			l.buckets[key] = b
		}
	}

	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// sweepLocked drops buckets that have been idle long enough to be full again (they carry no
// information). Runs at most once a minute, or immediately when the table is full.
func (l *Limiter) sweepLocked(now time.Time) {
	full := len(l.buckets) >= l.maxKeys
	if !full && now.Sub(l.lastScan) < time.Minute {
		return
	}
	l.lastScan = now
	idle := time.Duration(l.burst/l.rate*float64(time.Second)) + time.Second
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}

// Len reports the number of tracked keys (for tests and metrics).
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
