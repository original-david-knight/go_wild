package gowild_agent_net

import (
	"crypto/ed25519"
	"crypto/sha512"
	"fmt"

	"filippo.io/edwards25519"
)

// ed25519PrivateKeyToX25519 converts an Ed25519 private key to an X25519 private key.
// Uses the SHA-512 seed + clamp method per RFC 7748.
func ed25519PrivateKeyToX25519(privKey ed25519.PrivateKey) [32]byte {
	// Ed25519 private keys contain the seed in the first 32 bytes
	seed := privKey.Seed()
	h := sha512.Sum512(seed)

	// Clamp the first 32 bytes per X25519 spec
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

	// Parse the Ed25519 public key as an Edwards point
	p, err := new(edwards25519.Point).SetBytes(pubKey)
	if err != nil {
		return x25519Key, fmt.Errorf("invalid Ed25519 public key: %w", err)
	}

	// Convert Edwards point to Montgomery u-coordinate
	copy(x25519Key[:], p.BytesMontgomery())

	return x25519Key, nil
}
