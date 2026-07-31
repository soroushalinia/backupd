package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	streamChunkSize = 1 << 20 // 1 MiB plaintext per chunk
	streamHeader    = "BACKUPD-STREAM-ENC-1"
)

// StreamEncrypt encrypts r into w in chunks of 1 MiB with AES-256-GCM.
// Output layout: fixed header, 12-byte random nonce prefix, then a sequence
// of length-prefixed ciphertext chunks; a zero length marks the end. Memory
// use is bounded by the chunk size regardless of input size.
func StreamEncrypt(key []byte, r io.Reader, w io.Writer) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("new gcm: %w", err)
	}

	if _, err := io.WriteString(w, streamHeader); err != nil {
		return err
	}
	prefix := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, prefix); err != nil {
		return fmt.Errorf("nonce prefix: %w", err)
	}
	if _, err := w.Write(prefix); err != nil {
		return err
	}

	buf := make([]byte, streamChunkSize)
	lenBuf := make([]byte, 4)
	nonce := make([]byte, NonceSize)
	copy(nonce, prefix)
	var counter uint32
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			binary.BigEndian.PutUint32(nonce[8:], counter)
			counter++
			ct := aesgcm.Seal(nil, nonce, buf[:n], nil)
			binary.BigEndian.PutUint32(lenBuf, uint32(len(ct)))
			if _, err := w.Write(lenBuf); err != nil {
				return err
			}
			if _, err := w.Write(ct); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return err
		}
	}

	binary.BigEndian.PutUint32(lenBuf, 0)
	_, err = w.Write(lenBuf)
	return err
}

// StreamDecrypt reverses StreamEncrypt, writing plaintext to w.
func StreamDecrypt(key []byte, r io.Reader, w io.Writer) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("new gcm: %w", err)
	}

	header := make([]byte, len(streamHeader))
	if _, err := io.ReadFull(r, header); err != nil {
		return fmt.Errorf("reading header: %w", err)
	}
	if string(header) != streamHeader {
		return fmt.Errorf("invalid stream header")
	}

	prefix := make([]byte, 8)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return fmt.Errorf("reading nonce prefix: %w", err)
	}

	lenBuf := make([]byte, 4)
	buf := make([]byte, streamChunkSize+aesgcm.Overhead())
	nonce := make([]byte, NonceSize)
	copy(nonce, prefix)
	var counter uint32
	for {
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return fmt.Errorf("reading chunk length: %w", err)
		}
		n := binary.BigEndian.Uint32(lenBuf)
		if n == 0 {
			return nil
		}
		if n > uint32(len(buf)) {
			return fmt.Errorf("chunk too large: %d", n)
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return fmt.Errorf("reading chunk: %w", err)
		}
		nonce := make([]byte, NonceSize)
		copy(nonce, prefix)
		binary.BigEndian.PutUint32(nonce[8:], counter)
		counter++
		plain, err := aesgcm.Open(nil, nonce, buf[:n], nil)
		if err != nil {
			return fmt.Errorf("decrypt chunk: %w", err)
		}
		if _, err := w.Write(plain); err != nil {
			return err
		}
	}
}
