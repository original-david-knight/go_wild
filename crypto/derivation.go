package gowild_crypto

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
	"github.com/tyler-smith/go-bip39"
)

// DerivedKeys contains the derived private keys for both chains.
type DerivedKeys struct {
	EthPrivateKey string // Hex format with 0x prefix
	EthAddress    string // Ethereum address
	SolPrivateKey string // Base58 encoded keypair
	SolAddress    string // Solana public key (base58)
}

// DeriveKeysFromMnemonic derives Ethereum and Solana keys from a BIP39 mnemonic.
// The accountIndex determines which account to derive (0, 1, 2, etc.).
// Ethereum uses BIP44 path: m/44'/60'/0'/0/{accountIndex}
// Solana uses SLIP-0010 path: m/44'/501'/{accountIndex}'/0'
func DeriveKeysFromMnemonic(mnemonic string, accountIndex uint32) (*DerivedKeys, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic phrase")
	}

	seed := bip39.NewSeed(mnemonic, "")

	ethKey, ethAddr, err := deriveEthereumKey(seed, accountIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to derive ethereum key: %w", err)
	}

	solKey, solAddr, err := deriveSolanaKeySLIP10(seed, accountIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to derive solana key: %w", err)
	}

	return &DerivedKeys{
		EthPrivateKey: ethKey,
		EthAddress:    ethAddr,
		SolPrivateKey: solKey,
		SolAddress:    solAddr,
	}, nil
}

// GenerateMnemonic creates a new random BIP39 mnemonic phrase.
func GenerateMnemonic() (string, error) {
	entropy, err := bip39.NewEntropy(256) // 24 words
	if err != nil {
		return "", fmt.Errorf("failed to generate entropy: %w", err)
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("failed to generate mnemonic: %w", err)
	}
	return mnemonic, nil
}

// ValidateMnemonic checks if a mnemonic phrase is valid.
func ValidateMnemonic(mnemonic string) bool {
	return bip39.IsMnemonicValid(mnemonic)
}

// deriveEthereumKey derives an Ethereum private key using BIP44.
// Path: m/44'/60'/0'/0/{accountIndex}
func deriveEthereumKey(seed []byte, accountIndex uint32) (string, string, error) {
	masterKey, err := hdkeychain.NewMaster(seed, &chaincfg.MainNetParams)
	if err != nil {
		return "", "", err
	}

	// BIP44 path for Ethereum: m/44'/60'/0'/0/{accountIndex}
	path := []uint32{
		44 + hdkeychain.HardenedKeyStart, // purpose
		60 + hdkeychain.HardenedKeyStart, // coin type (ETH)
		0 + hdkeychain.HardenedKeyStart,  // account
		0,                                // change
		accountIndex,                     // address index
	}

	key := masterKey
	for _, child := range path {
		key, err = key.Derive(child)
		if err != nil {
			return "", "", err
		}
	}

	privKey, err := key.ECPrivKey()
	if err != nil {
		return "", "", err
	}

	ecdsaPrivKey := privKey.ToECDSA()
	hexKey := "0x" + hex.EncodeToString(crypto.FromECDSA(ecdsaPrivKey))
	address := crypto.PubkeyToAddress(ecdsaPrivKey.PublicKey).Hex()

	return hexKey, address, nil
}

// deriveSolanaKeySLIP10 derives a Solana private key using SLIP-0010 for ed25519.
// Path: m/44'/501'/{accountIndex}'/0'
func deriveSolanaKeySLIP10(seed []byte, accountIndex uint32) (string, string, error) {
	// SLIP-0010 master key derivation for ed25519
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(seed)
	I := mac.Sum(nil)
	key, chainCode := I[:32], I[32:]

	// Derive path: m/44'/501'/{accountIndex}'/0'
	path := []uint32{
		44 + 0x80000000,           // purpose (hardened)
		501 + 0x80000000,          // coin type (SOL, hardened)
		accountIndex + 0x80000000, // account (hardened)
		0 + 0x80000000,            // change (hardened)
	}

	for _, index := range path {
		buf := make([]byte, 37)
		buf[0] = 0x00
		copy(buf[1:33], key)
		binary.BigEndian.PutUint32(buf[33:], index)
		mac := hmac.New(sha512.New, chainCode)
		mac.Write(buf)
		I := mac.Sum(nil)
		key, chainCode = I[:32], I[32:]
	}

	// Create ed25519 keypair
	edPrivKey := ed25519.NewKeyFromSeed(key)
	pubKey := edPrivKey.Public().(ed25519.PublicKey)

	// Solana expects 64-byte keypair (32-byte private seed + 32-byte public key)
	keypair := make([]byte, 64)
	copy(keypair[:32], edPrivKey[:32])
	copy(keypair[32:], pubKey)

	return base58.Encode(keypair), base58.Encode(pubKey), nil
}
