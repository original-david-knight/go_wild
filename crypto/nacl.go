package gowild_crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"fmt"

	"filippo.io/edwards25519"
	"golang.org/x/crypto/nacl/box"
)

// ed25519PrivateKeyToX25519 converts an Ed25519 private key to an X25519 private key.
// Uses the SHA-512 seed + clamp method per RFC 7748.
func ed25519PrivateKeyToX25519(privKey ed25519.PrivateKey) [32]byte {
	seed := privKey.Seed()
	h := sha512.Sum512(seed)

	var x25519Key [32]byte
	copy(x25519Key[:], h[:32])
	x25519Key[0] &= 248
	x25519Key[31] &= 127
	x25519Key[31] |= 64

	return x25519Key
}

// ed25519PublicKeyToX25519 converts an Ed25519 public key to an X25519 public key.
// Performs Edwards-to-Montgomery point conversion using filippo.io/edwards25519.
func ed25519PublicKeyToX25519(pubKey ed25519.PublicKey) ([32]byte, error) {
	var x25519Key [32]byte

	p, err := new(edwards25519.Point).SetBytes(pubKey)
	if err != nil {
		return x25519Key, fmt.Errorf("invalid Ed25519 public key: %w", err)
	}

	copy(x25519Key[:], p.BytesMontgomery())

	return x25519Key, nil
}

// naclBoxSeal encrypts plaintext using NaCl box (XSalsa20-Poly1305 + X25519).
// Returns ciphertext and the random nonce used.
func naclBoxSeal(plaintext []byte, recipientX25519Pub, senderX25519Priv *[32]byte) (ciphertext []byte, nonce [24]byte, err error) {
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nonce, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext = box.Seal(nil, plaintext, &nonce, recipientX25519Pub, senderX25519Priv)
	return ciphertext, nonce, nil
}

// naclBoxOpen decrypts ciphertext using NaCl box (XSalsa20-Poly1305 + X25519).
func naclBoxOpen(ciphertext []byte, nonce *[24]byte, senderX25519Pub, recipientX25519Priv *[32]byte) ([]byte, error) {
	plaintext, ok := box.Open(nil, ciphertext, nonce, senderX25519Pub, recipientX25519Priv)
	if !ok {
		return nil, fmt.Errorf("decryption failed: invalid ciphertext or wrong key")
	}
	return plaintext, nil
}
