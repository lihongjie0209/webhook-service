package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const secretBytes = 32

type SecretBox struct {
	aead  cipher.AEAD
	keyID string
	rand  io.Reader
}

func NewSecretBox(encodedKey, keyID string) (*SecretBox, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("webhook encryption key must decode to 32 bytes")
	}
	if keyID == "" {
		return nil, errors.New("webhook encryption key ID is required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create webhook key cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create webhook AEAD: %w", err)
	}
	return &SecretBox{aead: aead, keyID: keyID, rand: rand.Reader}, nil
}

func (b *SecretBox) Generate() (plain string, encrypted []byte, keyID string, err error) {
	value := make([]byte, secretBytes)
	if _, err := io.ReadFull(b.rand, value); err != nil {
		return "", nil, "", fmt.Errorf("generate webhook signing secret: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(value)
	encrypted, err = b.Encrypt([]byte(plain))
	if err != nil {
		return "", nil, "", err
	}
	return plain, encrypted, b.keyID, nil
}

func (b *SecretBox) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(b.rand, nonce); err != nil {
		return nil, fmt.Errorf("generate webhook encryption nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plain, []byte(b.keyID)), nil
}

func (b *SecretBox) Decrypt(encrypted []byte, keyID string) ([]byte, error) {
	if keyID != b.keyID {
		return nil, fmt.Errorf("webhook encryption key %q is unavailable", keyID)
	}
	nonceSize := b.aead.NonceSize()
	if len(encrypted) < nonceSize {
		return nil, errors.New("webhook ciphertext is malformed")
	}
	plain, err := b.aead.Open(nil, encrypted[:nonceSize], encrypted[nonceSize:], []byte(keyID))
	if err != nil {
		return nil, errors.New("decrypt webhook signing secret")
	}
	return plain, nil
}
