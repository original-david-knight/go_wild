package gowild_crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestNaClBoxRoundTrip(t *testing.T) {
	// Generate Alice and Bob Ed25519 keypairs
	alicePub, alicePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Alice keypair: %v", err)
	}
	bobPub, bobPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Bob keypair: %v", err)
	}

	// Convert to X25519
	aliceX25519Priv := ed25519PrivateKeyToX25519(alicePriv)
	bobX25519Pub, err := ed25519PublicKeyToX25519(bobPub)
	if err != nil {
		t.Fatalf("failed to convert Bob public key: %v", err)
	}
	bobX25519Priv := ed25519PrivateKeyToX25519(bobPriv)
	aliceX25519Pub, err := ed25519PublicKeyToX25519(alicePub)
	if err != nil {
		t.Fatalf("failed to convert Alice public key: %v", err)
	}

	// Alice encrypts a message for Bob
	plaintext := []byte("Hello Bob, this is a secret message from Alice!")
	ciphertext, nonce, err := naclBoxSeal(plaintext, &bobX25519Pub, &aliceX25519Priv)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if len(ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}

	// Bob decrypts the message
	decrypted, err := naclBoxOpen(ciphertext, &nonce, &aliceX25519Pub, &bobX25519Priv)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted text doesn't match: got %q, want %q", decrypted, plaintext)
	}
}

func TestNaClBoxWrongKeyFails(t *testing.T) {
	// Generate Alice, Bob, and Eve keypairs
	_, alicePriv, _ := ed25519.GenerateKey(rand.Reader)
	bobPub, _, _ := ed25519.GenerateKey(rand.Reader)
	evePub, evePriv, _ := ed25519.GenerateKey(rand.Reader)

	aliceX25519Priv := ed25519PrivateKeyToX25519(alicePriv)
	bobX25519Pub, _ := ed25519PublicKeyToX25519(bobPub)
	eveX25519Priv := ed25519PrivateKeyToX25519(evePriv)
	eveX25519Pub, _ := ed25519PublicKeyToX25519(evePub)

	// Alice encrypts for Bob
	plaintext := []byte("Secret for Bob only")
	ciphertext, nonce, err := naclBoxSeal(plaintext, &bobX25519Pub, &aliceX25519Priv)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Eve tries to decrypt (should fail — wrong recipient key)
	_, err = naclBoxOpen(ciphertext, &nonce, &eveX25519Pub, &eveX25519Priv)
	if err == nil {
		t.Fatal("expected decryption to fail with Eve's key, but it succeeded")
	}
}

func TestNaClBoxEmptyMessage(t *testing.T) {
	alicePub, alicePriv, _ := ed25519.GenerateKey(rand.Reader)
	_, bobPriv, _ := ed25519.GenerateKey(rand.Reader)
	bobPub := bobPriv.Public().(ed25519.PublicKey)

	aliceX25519Priv := ed25519PrivateKeyToX25519(alicePriv)
	bobX25519Pub, _ := ed25519PublicKeyToX25519(bobPub)
	bobX25519Priv := ed25519PrivateKeyToX25519(bobPriv)
	aliceX25519Pub, _ := ed25519PublicKeyToX25519(alicePub)

	// Encrypt empty message
	ciphertext, nonce, err := naclBoxSeal([]byte{}, &bobX25519Pub, &aliceX25519Priv)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	decrypted, err := naclBoxOpen(ciphertext, &nonce, &aliceX25519Pub, &bobX25519Priv)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}

func TestEd25519KeyConversionConsistency(t *testing.T) {
	// Same key should always produce the same X25519 key
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)

	x1 := ed25519PrivateKeyToX25519(priv)
	x2 := ed25519PrivateKeyToX25519(priv)
	if x1 != x2 {
		t.Fatal("private key conversion is not deterministic")
	}

	xp1, _ := ed25519PublicKeyToX25519(pub)
	xp2, _ := ed25519PublicKeyToX25519(pub)
	if xp1 != xp2 {
		t.Fatal("public key conversion is not deterministic")
	}
}

func TestEd25519PublicKeyToX25519InvalidKey(t *testing.T) {
	_, err := ed25519PublicKeyToX25519([]byte{0, 1, 2, 3}) // too short
	if err == nil {
		t.Fatal("expected error for invalid public key")
	}
}
