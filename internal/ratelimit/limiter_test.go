package ratelimit

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"100", 100, false},
		{"500K", 500 * 1024, false},
		{"10M", 10 * 1024 * 1024, false},
		{"1G", 1024 * 1024 * 1024, false},
		{"2MiB", 2 * 1024 * 1024, false},
		{"-5", 0, true},
		{"fast", 0, true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.err {
			if err == nil {
				t.Errorf("Parse(%q) = %d, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestReaderThrottles(t *testing.T) {
	// 1000 B/s with 2500 bytes to read: the bucket starts full (1000
	// bytes of burst), so the minimum time is (2500-1000)/1000 = 1.5s.
	l := New(1000)
	ctx := context.Background()

	start := time.Now()
	rd := NewReader(ctx, &smallReadReader{r: io.LimitReader(zeroReader{}, 2500)}, l)
	if _, err := io.Copy(io.Discard, rd); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	// Allow generous slack for CI machines.
	if elapsed < 1400*time.Millisecond {
		t.Errorf("read 2500 bytes at 1000 B/s in %s, expected >= 1.5s", elapsed)
	}
	if elapsed > 6*time.Second {
		t.Errorf("read took %s, suspiciously slow", elapsed)
	}
}

func TestReaderUnlimitedWhenNilLimiter(t *testing.T) {
	rd := NewReader(context.Background(), io.LimitReader(zeroReader{}, 4096), nil)
	start := time.Now()
	if _, err := io.Copy(io.Discard, rd); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("unlimited read took %s", elapsed)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// smallReadReader caps each Read call at 100 bytes so the limiter's per-
// read accounting stays fine-grained, mirroring real stream behaviour.
type smallReadReader struct {
	r io.Reader
}

func (s *smallReadReader) Read(p []byte) (int, error) {
	if len(p) > 100 {
		p = p[:100]
	}
	return s.r.Read(p)
}
