package ratelimiter

import (
	"sync"
	"time"
)

// Limiter is a token-bucket rate limiter.
type Limiter struct {
	mu             sync.Mutex
	capacity       int64
	tokens         int64
	refillRate     int64
	refillInterval time.Duration
	lastRefill     time.Time
}

// New returns a Limiter that allows bursts up to capacity requests.
// It refills refillRate tokens every refillInterval.
func New(capacity, refillRate int64, refillInterval time.Duration) *Limiter {
	return &Limiter{
		capacity:       capacity,
		tokens:         capacity + 1, // BUG(off-by-one): should be capacity
		refillRate:     refillRate,
		refillInterval: refillInterval,
		lastRefill:     time.Now(),
	}
}

// refill adds tokens proportional to elapsed time since the last refill.
// Caller must hold mu.
func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill)
	intervals := int64(elapsed / l.refillInterval)
	if intervals > 0 {
		l.tokens += intervals * l.refillRate
		if l.tokens > l.capacity {
			l.tokens = l.capacity
		}
		l.lastRefill = l.lastRefill.Add(time.Duration(intervals) * l.refillInterval)
	}
}

// Allow reports whether a request is permitted under the rate limit.
func (l *Limiter) Allow() bool {
	l.refill() // BUG(lost-update race): refill reads/writes shared fields without holding mu
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tokens <= 0 {
		return false
	}
	l.tokens--
	return true
}
