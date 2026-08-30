package webhook

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestSecretBoxGenerateAndDecrypt(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	box, err := NewSecretBox(key, "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	box.rand = bytes.NewReader(bytes.Repeat([]byte{9}, 128))
	plain, encrypted, keyID, err := box.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if plain == "" || keyID != "test-v1" || bytes.Contains(encrypted, []byte(plain)) {
		t.Fatalf("generated secret metadata is invalid")
	}
	decrypted, err := box.Decrypt(encrypted, keyID)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != plain {
		t.Fatalf("Decrypt() = %q, want %q", decrypted, plain)
	}
}

func TestSecretBoxRejectsWrongKeyAndTampering(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	box, err := NewSecretBox(key, "test-v1")
	if err != nil {
		t.Fatal(err)
	}
	box.rand = bytes.NewReader(bytes.Repeat([]byte{3}, 64))
	encrypted, err := box.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.Decrypt(encrypted, "old-key"); err == nil {
		t.Fatal("Decrypt() accepted an unavailable key")
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := box.Decrypt(encrypted, "test-v1"); err == nil {
		t.Fatal("Decrypt() accepted tampered ciphertext")
	}
}

func TestNewSecretBoxRejectsInvalidKeys(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		keyID string
	}{
		{name: "malformed base64", key: "!", keyID: "v1"},
		{name: "wrong key length", key: base64.StdEncoding.EncodeToString([]byte("short")), keyID: "v1"},
		{name: "missing key ID", key: base64.StdEncoding.EncodeToString(make([]byte, 32))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSecretBox(test.key, test.keyID); err == nil {
				t.Fatal("NewSecretBox() accepted invalid configuration")
			}
		})
	}
}
