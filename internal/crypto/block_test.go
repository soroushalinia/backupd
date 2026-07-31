package crypto

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestEncryptBlockDeterministic(t *testing.T) {
	master := []byte("master-key-master-key")
	plaintext := bytes.Repeat([]byte("hello world"), 100)

	c1, err := EncryptBlock(master, plaintext)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}
	c2, err := EncryptBlock(master, plaintext)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}

	if !bytes.Equal(c1, c2) {
		t.Fatal("encrypting the same plaintext twice produced different ciphertexts")
	}
	if bytes.Equal(c1, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}
}

func TestEncryptBlockDifferentPlaintexts(t *testing.T) {
	master := []byte("master-key-master-key")
	a := []byte("block A content")
	b := []byte("block B content")

	ca, err := EncryptBlock(master, a)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}
	cb, err := EncryptBlock(master, b)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}
	if bytes.Equal(ca, cb) {
		t.Fatal("different plaintexts produced identical ciphertexts")
	}
}

func TestDecryptBlock(t *testing.T) {
	master := []byte("master-key-master-key")
	plaintext := []byte("block content to decrypt")

	ciphertext, err := EncryptBlock(master, plaintext)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}

	plainHash := sha256.Sum256(plaintext)
	ph := plainHash[:]
	got, err := DecryptBlock(master, ph, ciphertext)
	if err != nil {
		t.Fatalf("DecryptBlock: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatal("decrypted plaintext mismatch")
	}
}

func TestDecryptBlockWrongHash(t *testing.T) {
	master := []byte("master-key-master-key")
	plaintext := []byte("block content")

	ciphertext, err := EncryptBlock(master, plaintext)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}

	wrongHash := sha256.Sum256([]byte("tampered"))
	wh := wrongHash[:]
	if _, err := DecryptBlock(master, wh, ciphertext); err == nil {
		t.Fatal("expected error when authenticated hash does not match")
	}
}

func TestDecryptBlockWrongMaster(t *testing.T) {
	plaintext := []byte("block content")

	ciphertext, err := EncryptBlock([]byte("master-key-master-key"), plaintext)
	if err != nil {
		t.Fatalf("EncryptBlock: %v", err)
	}

	plainHash := sha256.Sum256(plaintext)
	ph := plainHash[:]
	if _, err := DecryptBlock([]byte("different-master-k"), ph, ciphertext); err == nil {
		t.Fatal("expected error when master key does not match")
	}
}
