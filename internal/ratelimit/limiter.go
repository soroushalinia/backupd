// Package ratelimit provides a simple token-bucket rate limiter used to
// throttle backup and restore network traffic.
package ratelimit

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limiter is a token bucket: a read of n bytes is allowed immediately if
// at least n tokens are available, otherwise the reader blocks until the
// bucket refills. The bucket holds up to one second of rate as burst.
type Limiter struct {
	mu       sync.Mutex
	rate     float64 // tokens per second
	tokens   float64
	lastFill time.Time
}

// New returns a limiter allowing rate bytes per second.
func New(rate int64) *Limiter {
	return &Limiter{
		rate:     float64(rate),
		tokens:   float64(rate),
		lastFill: time.Now(),
	}
}

func (l *Limiter) fill() {
	now := time.Now()
	elapsed := now.Sub(l.lastFill).Seconds()
	l.lastFill = now
	l.tokens += elapsed * l.rate
	if cap := l.rate; l.tokens > cap {
		l.tokens = cap
	}
}

// Wait blocks until n tokens are available.
func (l *Limiter) Wait(ctx context.Context, n int64) error {
	for {
		l.mu.Lock()
		l.fill()
		if l.tokens >= float64(n) {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return nil
		}
		need := float64(n) - l.tokens
		wait := time.Duration(need / l.rate * float64(time.Second))
		l.mu.Unlock()

		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Reader wraps an io.Reader and throttles reads to the limiter's rate. It
// forwards Close to the wrapped reader when present, so it can be used
// where an io.ReadCloser is required.
type Reader struct {
	r   io.Reader
	l   *Limiter
	ctx context.Context
}

// NewReader wraps r so that reads are throttled by l. A nil limiter
// returns r unchanged.
func NewReader(ctx context.Context, r io.Reader, l *Limiter) io.Reader {
	if l == nil {
		return r
	}
	return &Reader{r: r, l: l, ctx: ctx}
}

// WrapReadCloser wraps an io.ReadCloser so reads are throttled by l.
// A nil limiter returns r unchanged. Close is forwarded to r.
func WrapReadCloser(ctx context.Context, r io.ReadCloser, l *Limiter) io.ReadCloser {
	if l == nil {
		return r
	}
	return &Reader{r: r, l: l, ctx: ctx}
}

func (r *Reader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		if werr := r.l.Wait(r.ctx, int64(n)); werr != nil {
			return n, werr
		}
	}
	return n, err
}

// Close forwards to the wrapped reader when it implements io.Closer.
func (r *Reader) Close() error {
	if c, ok := r.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Parse converts a human-readable rate like "500K", "10M", or "1G" into
// bytes per second. A plain integer is taken as bytes per second. Units
// are powers of 1024; 0 or an empty string means unlimited.
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "0" {
		return 0, nil
	}

	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{
		{"tib", 1024 * 1024 * 1024 * 1024},
		{"gib", 1024 * 1024 * 1024},
		{"mib", 1024 * 1024},
		{"kib", 1024},
		{"tb", 1024 * 1024 * 1024 * 1024},
		{"gb", 1024 * 1024 * 1024},
		{"mb", 1024 * 1024},
		{"kb", 1024},
		{"t", 1024 * 1024 * 1024 * 1024},
		{"g", 1024 * 1024 * 1024},
		{"m", 1024 * 1024},
		{"k", 1024},
		{"b", 1},
	} {
		if strings.HasSuffix(s, u.suffix) {
			s = strings.TrimSuffix(s, u.suffix)
			mult = u.mult
			break
		}
	}

	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("rate must not be negative")
	}
	return n * mult, nil
}
