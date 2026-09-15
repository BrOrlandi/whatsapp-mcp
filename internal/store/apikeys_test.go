package store

import (
	"strings"
	"testing"
)

// A credential must be unguessable, recognisable, and stored only as a digest.
func TestNewAPIKeyShapeAndStorage(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		secret, digest, prefix, err := NewAPIKey()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(secret, KeyPrefix) {
			t.Fatalf("secret %q does not carry the prefix", secret)
		}
		body := strings.TrimPrefix(secret, KeyPrefix)
		if len(body) != 24 {
			t.Fatalf("secret body is %d characters, want 24", len(body))
		}
		for _, r := range body {
			if !strings.ContainsRune(keyAlphabet, r) {
				t.Fatalf("secret %q contains %q, which is outside the alphabet", secret, r)
			}
		}
		if seen[secret] {
			t.Fatalf("secret %q was generated twice", secret)
		}
		seen[secret] = true

		// What is stored must be the digest, never the secret.
		if digest == secret || digest != HashAPIKey(secret) {
			t.Fatal("the stored value is not the digest of the secret")
		}
		if len(digest) != 64 {
			t.Fatalf("digest is %d characters, want a 64-character SHA-256 hex", len(digest))
		}
		// The prefix exists only so the panel can name a key; it must not be
		// enough to reconstruct one.
		if !strings.HasPrefix(secret, prefix) || len(prefix) >= len(secret) {
			t.Fatalf("prefix %q is not a short lead of the secret", prefix)
		}
	}
}

func TestHashAPIKeyIsStable(t *testing.T) {
	if HashAPIKey("wamcp-abc") != HashAPIKey("wamcp-abc") {
		t.Fatal("the digest is not stable")
	}
	if HashAPIKey("wamcp-abc") == HashAPIKey("wamcp-abd") {
		t.Fatal("different secrets share a digest")
	}
}
