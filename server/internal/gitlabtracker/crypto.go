package gitlabtracker

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Cipher owns one or more 256-bit AES keys keyed by an operator-facing
// int16 version. Ciphertext layout on the wire is
//
//	[1B version][12B GCM nonce][ciphertext + 16B tag]
//
// which is the same format the migration's `token_ciphertext BYTEA`
// column expects. The version byte lets an operator rotate keys without a
// migration: add the new key with a higher version, redeploy, and every
// new Encrypt call writes under the new version while every Decrypt still
// finds the old rows.
type Cipher struct {
	// aeads[v] is the pre-initialised AEAD for key version v. We keep the
	// AEAD instead of the raw key so hot-path Encrypt/Decrypt calls do
	// not re-derive it.
	aeads map[int16]cipher.AEAD
	// latest is the highest version present in aeads. Encrypt always
	// writes under this version so rotation is one env var + redeploy,
	// not one env var + backfill.
	latest int16
}

// NewCipher builds a Cipher from a version→base64(32-byte-key) map. An
// empty map or any invalid key aborts construction so no code path ever
// falls back to plaintext storage. Callers that boot without
// GITLAB_TRACKER_KEY should log the missing config and refuse to accept
// tracker credentials for the lifetime of the process.
func NewCipher(keys map[int16]string) (*Cipher, error) {
	if len(keys) == 0 {
		return nil, errors.New("gitlabtracker: at least one key version is required")
	}
	c := &Cipher{aeads: make(map[int16]cipher.AEAD, len(keys))}
	versions := make([]int16, 0, len(keys))
	for v, b64 := range keys {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("gitlabtracker: version %d: base64 decode: %w", v, err)
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("gitlabtracker: version %d: key must be 32 bytes, got %d", v, len(raw))
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return nil, fmt.Errorf("gitlabtracker: version %d: aes cipher: %w", v, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("gitlabtracker: version %d: gcm: %w", v, err)
		}
		c.aeads[v] = aead
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	c.latest = versions[len(versions)-1]
	return c, nil
}

// LatestVersion is exposed so tests and admin surfaces can confirm which
// key version new writes will land under.
func (c *Cipher) LatestVersion() int16 { return c.latest }

// Encrypt seals plaintext under the latest key version and returns the
// full versioned ciphertext blob. The blob is safe to write directly into
// gitlab_tracker_connection.{token_ciphertext,webhook_secret_ciphertext}.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	aead := c.aeads[c.latest]
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("gitlabtracker: nonce: %w", err)
	}
	// Layout: [version] [nonce] [ciphertext+tag]. We stamp the version
	// as a single byte so the storage cost is one byte per row; int16 in
	// the DB schema still round-trips because we only use the low byte
	// (versions >= 256 would be a schema-level operator error we surface
	// on the read path).
	out := make([]byte, 1, 1+len(nonce)+len(plaintext)+aead.Overhead())
	out[0] = byte(c.latest)
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt takes a versioned blob and returns the plaintext or an error.
// The AEAD auth tag catches every corruption; the version byte tells us
// which key to hand it — so a mismatched or absent key surfaces as an
// unambiguous "unknown version" rather than a spurious "auth failure".
func (c *Cipher) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) < 1 {
		return nil, errors.New("gitlabtracker: ciphertext too short (missing version)")
	}
	version := int16(blob[0])
	aead, ok := c.aeads[version]
	if !ok {
		return nil, fmt.Errorf("gitlabtracker: unknown key version %d", version)
	}
	nonceSize := aead.NonceSize()
	if len(blob) < 1+nonceSize+aead.Overhead() {
		return nil, errors.New("gitlabtracker: ciphertext too short (missing nonce or tag)")
	}
	nonce := blob[1 : 1+nonceSize]
	body := blob[1+nonceSize:]
	plaintext, err := aead.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("gitlabtracker: decrypt: %w", err)
	}
	return plaintext, nil
}
