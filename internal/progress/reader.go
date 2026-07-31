package progress

import (
	"fmt"
	"io"
	"log"
	"time"
)

type Reader struct {
	r      io.Reader
	name   string
	total  int64
	last   time.Time
	report int64
	done   chan struct{}
}

func NewReader(r io.Reader, name string) *Reader {
	return &Reader{
		r:      r,
		name:   name,
		last:   time.Now(),
		report: 10 * 1024 * 1024,
		done:   make(chan struct{}),
	}
}

func (p *Reader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.total += int64(n)
		if elapsed := time.Since(p.last); elapsed > 5*time.Second || p.total >= p.report {
			log.Printf("  progress [%s]: %s uploaded (%.1f MB/s)",
				p.name, formatBytes(p.total), mbPerSec(p.total, time.Since(p.last)))
			p.report = p.total + 10*1024*1024
			p.last = time.Now()
		}
	}
	return n, err
}

func (p *Reader) Done() {
	log.Printf("  done [%s]: %s total", p.name, formatBytes(p.total))
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GiB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MiB", float64(b)/(1024*1024))
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}

func mbPerSec(b int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(b) / (1024 * 1024) / d.Seconds()
}
