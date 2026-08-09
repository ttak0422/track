package site

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// The published data bundle is locked (ADR 0069): every file under <out>/data is gzipped, encrypted
// with AES-256-GCM, and written as raw bytes (<name>.bin) instead of readable JSON. The lock is not
// secrecy — the key travels in the page (window.__trackLock) because the reader's own browser has to
// open the data — it is a door: the bytes a crawler downloads are not usable data until it performs a
// specific, deliberate conversion with the site's key.
//
// Layout: nonce (12 bytes) || AES-256-GCM(gzip(plaintext)).
//
// The key is derived from the site's public identity so a rebuild of the same site keeps the same key,
// and two different sites do not share one.
func LockKey(baseURL, title string) []byte {
	sum := sha256.Sum256([]byte("track-site-lock\x00" + baseURL + "\x00" + title))
	return sum[:]
}

// LockKeyString is the key as the page carries it (base64, window.__trackLock).
func LockKeyString(key []byte) string { return base64.StdEncoding.EncodeToString(key) }

// lock gzips then encrypts one data file's bytes.
func lock(key, plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	gcm, err := lockCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, buf.Bytes(), nil), nil
}

// Unlock reverses lock: the operation a reader (the frontend, this repo's tests, anyone holding the
// site's key) performs to get the data back.
func Unlock(key, blob []byte) ([]byte, error) {
	gcm, err := lockCipher(key)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, fmt.Errorf("locked data too short")
	}
	packed, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func lockCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
