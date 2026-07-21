package gowild_agent_net

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

func TestEd25519PrivateKeyToX25519(t *testing.T) {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	x25519Key := ed25519PrivateKeyToX25519(privKey)

	// Should produce a 32-byte key
	if len(x25519Key) != 32 {
		t.Errorf("Expected 32-byte key, got %d bytes", len(x25519Key))
	}

	// Should be deterministic
	x25519Key2 := ed25519PrivateKeyToX25519(privKey)
	if x25519Key != x25519Key2 {
		t.Error("Key conversion should be deterministic")
	}

	// Verify clamping
	if x25519Key[0]&7 != 0 {
		t.Error("Low 3 bits of first byte should be cleared")
	}
	if x25519Key[31]&128 != 0 {
		t.Error("High bit of last byte should be cleared")
	}
	if x25519Key[31]&64 == 0 {
		t.Error("Second-highest bit of last byte should be set")
	}
}

func TestEd25519PublicKeyToX25519(t *testing.T) {
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	x25519Key, err := ed25519PublicKeyToX25519(pubKey)
	if err != nil {
		t.Fatalf("Failed to convert public key: %v", err)
	}

	if len(x25519Key) != 32 {
		t.Errorf("Expected 32-byte key, got %d bytes", len(x25519Key))
	}

	// Should be deterministic
	x25519Key2, err := ed25519PublicKeyToX25519(pubKey)
	if err != nil {
		t.Fatalf("Second conversion failed: %v", err)
	}
	if x25519Key != x25519Key2 {
		t.Error("Key conversion should be deterministic")
	}
}

func TestEd25519PublicKeyToX25519_InvalidKey(t *testing.T) {
	// Use a key that's too short to be valid
	shortKey := make(ed25519.PublicKey, 16)
	_, err := ed25519PublicKeyToX25519(shortKey)
	if err == nil {
		t.Error("Expected error for invalid public key (wrong length)")
	}
}

func TestCryptoRoundTrip(t *testing.T) {
	// Generate two Ed25519 keypairs (sender and recipient)
	senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate sender key: %v", err)
	}

	recipientPub, recipientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate recipient key: %v", err)
	}

	// Convert to X25519
	senderX25519Priv := ed25519PrivateKeyToX25519(senderPriv)
	senderX25519Pub, err := ed25519PublicKeyToX25519(senderPub)
	if err != nil {
		t.Fatalf("Failed to convert sender public key: %v", err)
	}

	recipientX25519Priv := ed25519PrivateKeyToX25519(recipientPriv)
	recipientX25519Pub, err := ed25519PublicKeyToX25519(recipientPub)
	if err != nil {
		t.Fatalf("Failed to convert recipient public key: %v", err)
	}

	// Encrypt message from sender to recipient
	plaintext := []byte("Hello from agent to agent!")
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("Failed to generate nonce: %v", err)
	}

	ciphertext := box.Seal(nil, plaintext, &nonce, &recipientX25519Pub, &senderX25519Priv)

	// Decrypt on recipient side
	decrypted, ok := box.Open(nil, ciphertext, &nonce, &senderX25519Pub, &recipientX25519Priv)
	if !ok {
		t.Fatal("Failed to decrypt message")
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("Decrypted message mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestCryptoRoundTrip_WrongKey(t *testing.T) {
	// Generate three keypairs
	_, senderPriv, _ := ed25519.GenerateKey(rand.Reader)
	recipientPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, wrongPriv, _ := ed25519.GenerateKey(rand.Reader)

	senderX25519Priv := ed25519PrivateKeyToX25519(senderPriv)
	recipientX25519Pub, _ := ed25519PublicKeyToX25519(recipientPub)

	wrongX25519Priv := ed25519PrivateKeyToX25519(wrongPriv)
	wrongX25519Pub, _ := ed25519PublicKeyToX25519(recipientPub) // wrong priv, right pub target

	_ = wrongX25519Pub

	plaintext := []byte("Secret message")
	var nonce [24]byte
	rand.Read(nonce[:])

	ciphertext := box.Seal(nil, plaintext, &nonce, &recipientX25519Pub, &senderX25519Priv)

	// Try to decrypt with wrong private key — should fail
	_, ok := box.Open(nil, ciphertext, &nonce, &recipientX25519Pub, &wrongX25519Priv)
	if ok {
		t.Error("Decryption should fail with wrong private key")
	}
}
