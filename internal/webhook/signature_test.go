package webhook

import (
	"testing"
	"time"
)

func TestSignAndVerifySignature(t *testing.T) {
	secret := []byte("a-secret-known-only-to-the-subscriber")
	timestamp := time.Unix(1_700_000_000, 0)
	body := []byte(`{"event_id":"event-1"}`)
	signature := Sign(secret, timestamp, body)
	if signature != "v1=f28db8f9fedc47091fb69eaa12c24221c54e1bec2a7141f60cf6d755552ff27a" {
		t.Fatalf("Sign() = %q", signature)
	}
	if !VerifySignature(secret, timestamp, body, signature) {
		t.Fatal("VerifySignature() rejected valid signature")
	}
	if VerifySignature(secret, timestamp.Add(time.Second), body, signature) || VerifySignature(secret, timestamp, []byte("changed"), signature) {
		t.Fatal("VerifySignature() accepted changed signed material")
	}
}
