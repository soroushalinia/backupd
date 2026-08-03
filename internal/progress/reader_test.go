package progress

import (
	"bytes"
	"io"
	"testing"
)

func TestReaderPassesThrough(t *testing.T) {
	payload := bytes.Repeat([]byte("data"), 4096)
	pr := NewReader(bytes.NewReader(payload), "test")

	var got bytes.Buffer
	if _, err := io.Copy(&got, pr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), payload) {
		t.Error("reader corrupted the stream")
	}
	if pr.total != int64(len(payload)) {
		t.Errorf("total = %d, want %d", pr.total, len(payload))
	}
}

func TestReaderReadsExactChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("abc"), 1000)
	pr := NewReader(bytes.NewReader(payload), "test")

	buf := make([]byte, 1024)
	var read int64
	for {
		n, err := pr.Read(buf)
		read += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if read != int64(len(payload)) {
		t.Errorf("read %d bytes, want %d", read, len(payload))
	}
}

func TestReaderEmpty(t *testing.T) {
	pr := NewReader(bytes.NewReader(nil), "empty")
	n, err := pr.Read(make([]byte, 16))
	if n != 0 || err != io.EOF {
		t.Errorf("empty stream: got (%d, %v), want (0, EOF)", n, err)
	}
	pr.Done()
	if pr.total != 0 {
		t.Errorf("total = %d, want 0", pr.total)
	}
}
