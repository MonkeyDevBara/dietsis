package limit

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func TestBurstThenDeny(t *testing.T) {
	c := newClock()
	l := newWithClock(60, 3, 100, c.now) // 1 token/second, burst 3
	for i := range 3 {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("request %d within burst was denied", i+1)
		}
	}
	ok, retry := l.Allow("ip")
	if ok {
		t.Fatal("4th request must be denied")
	}
	if retry <= 0 || retry > time.Second+time.Millisecond {
		t.Fatalf("retryAfter = %v, want about 1s", retry)
	}
}

func TestRefillOverTime(t *testing.T) {
	c := newClock()
	l := newWithClock(60, 2, 100, c.now)
	l.Allow("k")
	l.Allow("k")
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("bucket should be empty")
	}
	c.advance(1100 * time.Millisecond)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("one token should have refilled after 1.1s")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("only one token should have refilled")
	}
	c.advance(time.Hour)
	for range 2 {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatal("refill must cap at burst, not below")
		}
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("refill must not exceed burst")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	c := newClock()
	l := newWithClock(60, 1, 100, c.now)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a first")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a second must be denied")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b must not be affected by a")
	}
}

func TestIdleBucketsAreSwept(t *testing.T) {
	c := newClock()
	l := newWithClock(60, 5, 100, c.now)
	for i := range 20 {
		l.Allow(fmt.Sprintf("ip-%d", i))
	}
	if l.Len() != 20 {
		t.Fatalf("Len = %d", l.Len())
	}
	c.advance(10 * time.Minute)
	l.Allow("fresh")
	if l.Len() != 1 {
		t.Fatalf("idle buckets should be swept, Len = %d", l.Len())
	}
}

func TestMemoryIsBoundedUnderKeyFlood(t *testing.T) {
	c := newClock()
	l := newWithClock(60, 1, 10, c.now)
	for i := range 1000 {
		l.Allow(fmt.Sprintf("spoofed-%d", i))
	}
	if l.Len() > 11 { // maxKeys + the shared overflow bucket
		t.Fatalf("table grew to %d keys with maxKeys=10", l.Len())
	}
	// Once full, unseen keys share one bucket: a flood of fresh keys cannot bypass the limit.
	denied := 0
	for i := range 50 {
		if ok, _ := l.Allow(fmt.Sprintf("another-%d", i)); !ok {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("flood of new keys must eventually be denied via the overflow bucket")
	}
}
