package gitlabtracker

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// helpers ----------------------------------------------------------------

// randomKey returns a valid 32-byte AES key encoded as base64, the same
// shape operators must set GITLAB_TRACKER_KEY to.
func randomKey(t *testing.T, seed byte) string {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed + byte(i)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// TestNewCipherRejectsMissingKey pins the fail-closed contract: an
// empty key map must be rejected at construction time so no code path
// ever falls back to plaintext storage.
func TestNewCipherRejectsMissingKey(t *testing.T) {
	if _, err := NewCipher(nil); err == nil {
		t.Fatal("NewCipher(nil) must reject an empty key map")
	}
	if _, err := NewCipher(map[int16]string{}); err == nil {
		t.Fatal("NewCipher(empty) must reject an empty key map")
	}
}

// TestNewCipherRejectsInvalidBase64 fails when an operator ships a
// mistyped key, rather than silently truncating it to a shorter (or
// larger) byte string.
func TestNewCipherRejectsInvalidBase64(t *testing.T) {
	_, err := NewCipher(map[int16]string{1: "not-base64!!"})
	if err == nil {
		t.Fatal("NewCipher must reject non-base64 material")
	}
	// Wrong length base64 (16 bytes decoded → 128-bit key, unsupported
	// by our 256-bit contract).
	_, err = NewCipher(map[int16]string{1: base64.StdEncoding.EncodeToString(make([]byte, 16))})
	if err == nil {
		t.Fatal("NewCipher must reject non-32-byte keys")
	}
}

// TestNewCipherAcceptsMultipleVersions is the rotation contract: the
// cipher accepts a map keyed by version so an operator can decrypt with
// the old key while re-encrypting with the new one.
func TestNewCipherAcceptsMultipleVersions(t *testing.T) {
	c, err := NewCipher(map[int16]string{
		1: randomKey(t, 1),
		2: randomKey(t, 100),
	})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if got := c.LatestVersion(); got != 2 {
		t.Fatalf("LatestVersion = %d, want 2", got)
	}
}

// TestEncryptDecryptRoundTrip validates the happy path AND the ciphertext
// layout (version byte / nonce / tag) required by everyone else's decode.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(map[int16]string{1: randomKey(t, 1)})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("glpat-abcdef123456")

	ct, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if len(ct) <= 1+12+16 {
		// version + nonce + tag with no payload would still be at
		// least (1 + 12 + tag). Anything shorter is a clear bug.
		t.Fatalf("ciphertext too short: %d bytes", len(ct))
	}
	if ct[0] != 1 {
		t.Fatalf("version byte = %d, want 1 (latest)", ct[0])
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatal("ciphertext must not contain the plaintext verbatim")
	}

	out, err := c.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(out, plaintext) {
		t.Fatalf("Decrypt = %q, want %q", out, plaintext)
	}
}

// TestDecryptWrongKeyFails proves the auth tag: ciphertext produced with
// one 32-byte secret does not decrypt under any other key.
func TestDecryptWrongKeyFails(t *testing.T) {
	c1, _ := NewCipher(map[int16]string{1: randomKey(t, 1)})
	c2, _ := NewCipher(map[int16]string{1: randomKey(t, 200)})
	ct, err := c1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := c2.Decrypt(ct); err == nil {
		t.Fatal("Decrypt with wrong key must fail")
	}
}

// TestDecryptTamperedCiphertextFails proves the auth tag catches
// modifications to any byte of the ciphertext (including the nonce
// segment) — a corrupted `token_ciphertext` column must never silently
// yield a bogus token.
func TestDecryptTamperedCiphertextFails(t *testing.T) {
	c, _ := NewCipher(map[int16]string{1: randomKey(t, 1)})
	ct, err := c.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip a bit deep inside the ciphertext body (past the version + nonce).
	tampered := append([]byte(nil), ct...)
	pos := 1 + 12 + 3
	tampered[pos] ^= 0x01
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt of tampered ciphertext must fail")
	}
	// Truncated ciphertext (loses part of the auth tag) must also fail.
	if _, err := c.Decrypt(ct[:len(ct)-2]); err == nil {
		t.Fatal("Decrypt of truncated ciphertext must fail")
	}
	// Unknown version byte must fail with a helpful error rather than
	// falling through to the default key.
	unknown := append([]byte(nil), ct...)
	unknown[0] = 99
	_, err = c.Decrypt(unknown)
	if err == nil {
		t.Fatal("Decrypt with unknown version must fail")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error %q should mention version", err.Error())
	}
}

// TestDecryptMultiVersionRotation proves the rotation flow: after adding a
// newer key, ciphertext written by the older key still decrypts and new
// ciphertext lands under the newer key.
func TestDecryptMultiVersionRotation(t *testing.T) {
	old, _ := NewCipher(map[int16]string{1: randomKey(t, 1)})
	oldCT, err := old.Encrypt([]byte("legacy"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	rotated, err := NewCipher(map[int16]string{
		1: randomKey(t, 1),
		2: randomKey(t, 100),
	})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if out, err := rotated.Decrypt(oldCT); err != nil {
		t.Fatalf("old ciphertext should decrypt after rotation: %v", err)
	} else if string(out) != "legacy" {
		t.Fatalf("Decrypt legacy = %q, want %q", out, "legacy")
	}
	newCT, err := rotated.Encrypt([]byte("new"))
	if err != nil {
		t.Fatalf("Encrypt after rotation: %v", err)
	}
	if newCT[0] != 2 {
		t.Fatalf("post-rotation ciphertext version = %d, want 2", newCT[0])
	}
}
