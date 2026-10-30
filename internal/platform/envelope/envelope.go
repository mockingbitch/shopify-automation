// Package envelope encrypts secrets at rest with envelope encryption.
//
// Each database row gets its own random data key (DEK). Column values are
// sealed with AES-256-GCM under that DEK, and the DEK itself is sealed
// ("wrapped") under a key-encryption key (KEK) from the Keyring. Only the
// wrapped DEK and the KEK ID are stored next to the ciphertext, so rotating a
// KEK means re-wrapping DEKs, not re-encrypting data.
//
// Every seal binds additional authenticated data (AAD) built from the table,
// the row ID and the column, so ciphertext copied to another row or column
// fails to decrypt.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const keySize = 32

// ErrDecrypt is returned for any failure to open ciphertext. It does not say
// why (wrong key, wrong row, tampering), on purpose.
var ErrDecrypt = errors.New("envelope: cannot decrypt")

var keyIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Keyring holds the key-encryption keys. New data is wrapped with the active
// key; older keys stay available for decryption until rotation completes.
type Keyring struct {
	active string
	keys   map[string][]byte
}

// ParseKeyring parses "id:base64key[,id:base64key...]" and selects active.
// Keys must be 32 bytes (standard or URL-safe base64).
func ParseKeyring(spec, active string) (*Keyring, error) {
	k := &Keyring{active: active, keys: map[string][]byte{}}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, enc, ok := strings.Cut(part, ":")
		if !ok || !keyIDRe.MatchString(id) {
			return nil, errors.New("envelope: each key must be id:base64 with a short alphanumeric id")
		}
		raw, err := decodeKey(enc)
		if err != nil || len(raw) != keySize {
			return nil, fmt.Errorf("envelope: key %q must be %d bytes of base64", id, keySize)
		}
		if _, dup := k.keys[id]; dup {
			return nil, fmt.Errorf("envelope: duplicate key id %q", id)
		}
		k.keys[id] = raw
	}
	if len(k.keys) == 0 {
		return nil, errors.New("envelope: no keys configured")
	}
	if active == "" && len(k.keys) == 1 {
		for id := range k.keys {
			k.active = id
		}
	}
	if _, ok := k.keys[k.active]; !ok {
		return nil, fmt.Errorf("envelope: active key %q is not in the keyring", k.active)
	}
	return k, nil
}

func decodeKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("invalid base64")
}

// NewKeyring builds a keyring from raw keys (tests and tools).
func NewKeyring(active string, keys map[string][]byte) (*Keyring, error) {
	parts := make([]string, 0, len(keys))
	for id, key := range keys {
		parts = append(parts, id+":"+base64.StdEncoding.EncodeToString(key))
	}
	sort.Strings(parts)
	return ParseKeyring(strings.Join(parts, ","), active)
}

// GenerateKey returns a new random 32-byte key, base64 encoded.
func GenerateKey() string {
	b := make([]byte, keySize)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("envelope: random source failed: %w", err))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// ActiveID returns the ID of the key used for new data.
func (k *Keyring) ActiveID() string { return k.active }

// WrappedKey is a row's sealed DEK and the KEK that sealed it.
type WrappedKey struct {
	KEKID string
	DEK   []byte
}

// Row encrypts and decrypts the columns of one database row.
type Row struct {
	dek    []byte
	prefix string
}

func rowAAD(table, rowID string) string { return table + "|" + rowID }

// NewRow creates a fresh DEK for (table, rowID) and wraps it with the active KEK.
func (k *Keyring) NewRow(table, rowID string) (Row, WrappedKey, error) {
	dek := make([]byte, keySize)
	if _, err := rand.Read(dek); err != nil {
		return Row{}, WrappedKey{}, fmt.Errorf("envelope: generate dek: %w", err)
	}
	prefix := rowAAD(table, rowID)
	wrapped, err := seal(k.keys[k.active], dek, []byte("dek|"+prefix))
	if err != nil {
		return Row{}, WrappedKey{}, err
	}
	return Row{dek: dek, prefix: prefix}, WrappedKey{KEKID: k.active, DEK: wrapped}, nil
}

// OpenRow unwraps the DEK stored for (table, rowID).
func (k *Keyring) OpenRow(table, rowID string, w WrappedKey) (Row, error) {
	kek, ok := k.keys[w.KEKID]
	if !ok {
		return Row{}, ErrDecrypt
	}
	prefix := rowAAD(table, rowID)
	dek, err := open(kek, w.DEK, []byte("dek|"+prefix))
	if err != nil || len(dek) != keySize {
		return Row{}, ErrDecrypt
	}
	return Row{dek: dek, prefix: prefix}, nil
}

// Rewrap re-seals a row's DEK under the active KEK (rotation). The column
// ciphertext does not change.
func (k *Keyring) Rewrap(table, rowID string, w WrappedKey) (WrappedKey, error) {
	row, err := k.OpenRow(table, rowID, w)
	if err != nil {
		return WrappedKey{}, err
	}
	wrapped, err := seal(k.keys[k.active], row.dek, []byte("dek|"+row.prefix))
	if err != nil {
		return WrappedKey{}, err
	}
	return WrappedKey{KEKID: k.active, DEK: wrapped}, nil
}

// Seal encrypts plaintext for column.
func (r Row) Seal(column string, plaintext []byte) ([]byte, error) {
	if r.dek == nil {
		return nil, errors.New("envelope: row is not initialized")
	}
	return seal(r.dek, plaintext, []byte(r.prefix+"|"+column))
}

// Open decrypts ciphertext of column.
func (r Row) Open(column string, ciphertext []byte) ([]byte, error) {
	if r.dek == nil {
		return nil, ErrDecrypt
	}
	b, err := open(r.dek, ciphertext, []byte(r.prefix+"|"+column))
	if err != nil {
		return nil, ErrDecrypt
	}
	return b, nil
}

// seal returns nonce || AES-256-GCM(key, plaintext, aad).
func seal(key, plaintext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("envelope: nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func open(key, data, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrDecrypt
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("envelope: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
