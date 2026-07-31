package crypto

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestStreamRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plaintext := []byte("hello world this is test data")

	var enc bytes.Buffer
	if err := StreamEncrypt(key, bytes.NewReader(plaintext), &enc); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc.Bytes(), plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}

	var dec bytes.Buffer
	if err := StreamDecrypt(key, &enc, &dec); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, dec.Bytes()) {
		t.Fatalf("round-trip mismatch: got %q", dec.Bytes())
	}
}

func TestStreamRoundTripEmpty(t *testing.T) {
	key := make([]byte, KeySize)

	var enc bytes.Buffer
	if err := StreamEncrypt(key, bytes.NewReader(nil), &enc); err != nil {
		t.Fatal(err)
	}
	var dec bytes.Buffer
	if err := StreamDecrypt(key, &enc, &dec); err != nil {
		t.Fatal(err)
	}
	if dec.Len() != 0 {
		t.Fatal("expected empty plaintext")
	}
}

func TestStreamRoundTripMultiChunk(t *testing.T) {
	key := make([]byte, KeySize)
	plaintext := bytes.Repeat([]byte("abcdef"), 3*streamChunkSize+17) // > 3 chunks

	var enc bytes.Buffer
	if err := StreamEncrypt(key, bytes.NewReader(plaintext), &enc); err != nil {
		t.Fatal(err)
	}

	var dec bytes.Buffer
	if err := StreamDecrypt(key, &enc, &dec); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, dec.Bytes()) {
		t.Fatal("multi-chunk round-trip mismatch")
	}
}

func TestStreamDecryptWrongKey(t *testing.T) {
	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("fedcba9876543210fedcba9876543210")

	var enc bytes.Buffer
	if err := StreamEncrypt(key1, strings.NewReader("secret"), &enc); err != nil {
		t.Fatal(err)
	}
	if err := StreamDecrypt(key2, &enc, io.Discard); err == nil {
		t.Fatal("expected error with wrong key")
	}
}

func TestStreamDecryptTampered(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	var enc bytes.Buffer
	if err := StreamEncrypt(key, strings.NewReader("secret data"), &enc); err != nil {
		t.Fatal(err)
	}

	tampered := enc.Bytes()
	tampered[len(tampered)/2] ^= 0xff
	if err := StreamDecrypt(key, bytes.NewReader(tampered), io.Discard); err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}

func TestStreamDecryptBadHeader(t *testing.T) {
	key := make([]byte, KeySize)
	if err := StreamDecrypt(key, strings.NewReader("garbage"), io.Discard); err == nil {
		t.Fatal("expected error for invalid header")
	}
}

func TestKeyDerivation(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatal(err)
	}

	if len(salt) != SaltSize {
		t.Fatalf("salt size = %d, want %d", len(salt), SaltSize)
	}

	key1 := DeriveKey("mypassphrase", salt)
	key2 := DeriveKey("mypassphrase", salt)
	key3 := DeriveKey("different", salt)

	if len(key1) != KeySize {
		t.Fatalf("key size = %d, want %d", len(key1), KeySize)
	}

	if !bytes.Equal(key1, key2) {
		t.Fatal("same passphrase + salt should produce same key")
	}

	if bytes.Equal(key1, key3) {
		t.Fatal("different passphrase should produce different key")
	}
}

func TestKeyDerivationDeterministic(t *testing.T) {
	salt := []byte("fixed-salt-123!!")
	key1 := DeriveKey("pass", salt)
	key2 := DeriveKey("pass", salt)

	if !bytes.Equal(key1, key2) {
		t.Fatal("deterministic key derivation failed")
	}
}
