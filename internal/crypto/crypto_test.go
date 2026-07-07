package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plaintext := []byte("hello world this is test data")

	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(encrypted, plaintext) {
		t.Fatal("encrypted data matches plaintext")
	}

	if len(encrypted) <= NonceSize {
		t.Fatal("encrypted data too short")
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptDecryptEmpty(t *testing.T) {
	key := make([]byte, KeySize)
	encrypted, err := Encrypt(key, []byte{})
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if len(decrypted) != 0 {
		t.Fatal("expected empty result")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("fedcba9876543210fedcba9876543210")

	encrypted, _ := Encrypt(key1, []byte("secret"))
	_, err := Decrypt(key2, encrypted)
	if err == nil {
		t.Fatal("expected error with wrong key")
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

func TestEncryptDecryptLargeData(t *testing.T) {
	key := make([]byte, KeySize)
	plaintext := make([]byte, 100000)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatal("large data round-trip failed")
	}
}

func TestShortCiphertext(t *testing.T) {
	key := make([]byte, KeySize)
	_, err := Decrypt(key, []byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestKeySize(t *testing.T) {
	key := []byte("short")
	_, err := Encrypt(key, []byte("data"))
	if err == nil {
		t.Fatal("expected error with short key")
	}
}
