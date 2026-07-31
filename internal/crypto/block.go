package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

var (
	blockKeyLabel   = []byte("backupd-block-key")
	blockNonceLabel = []byte("backupd-block-nonce")
)

func blockKey(master, plainHash []byte) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write(blockKeyLabel)
	mac.Write(plainHash)
	return mac.Sum(nil)
}

func blockNonce(master, plainHash []byte) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write(blockNonceLabel)
	mac.Write(plainHash)
	return mac.Sum(nil)[:NonceSize]
}

// EncryptBlock encrypts a single deduplication block. The key and nonce are
// derived deterministically from the master key and the plaintext hash, so the
// same plaintext always produces the same ciphertext and deduplication is
// preserved. The plaintext hash is bound as authenticated data, so a mismatch
// is detected during decryption.
func EncryptBlock(master, plaintext []byte) ([]byte, error) {
	plainHash := sha256.Sum256(plaintext)
	block, err := aes.NewCipher(blockKey(master, plainHash[:]))
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return aesgcm.Seal(nil, blockNonce(master, plainHash[:]), plaintext, plainHash[:]), nil
}

// DecryptBlock reverses EncryptBlock. plainHash must be the sha256 of the
// original plaintext; it is verified as the authenticated data.
func DecryptBlock(master, plainHash, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(blockKey(master, plainHash))
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	plaintext, err := aesgcm.Open(nil, blockNonce(master, plainHash), ciphertext, plainHash)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
