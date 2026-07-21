# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build the package
go build ./...

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -run TestWalletSignMessage -v
```

## Architecture

Multi-chain cryptocurrency wallet library providing a unified interface for Ethereum and Solana operations. Private keys are encapsulated — never exposed through the public API.

### Core Components

**Wallet Abstraction** (`wallet.go`) - Unified interface wrapping both chains:
- `NewWallet(config)` / `NewWalletFromEnv()` - Create from config or environment
- `GetAddress(chain)` - Public address for a chain
- `SignMessage(chain, message)` - EIP-191 (ETH) or Ed25519 (SOL) signatures
- `GetBalance(ctx, chain)` / `GetTokenBalance(ctx, chain, token)` - Native and token balances
- `SendToken(ctx, chain, to, amount, token, memo)` - Transfer tokens
- `SwapToken(ctx, chain, from, to, amount, slippage)` - DEX swap (0x for ETH, Jupiter for SOL)
- `ContractCall(ctx, chain, contract, method, args, value, readOnly)` - Smart contract interaction
- `EncryptMessage(plaintext, recipientPubKey)` / `DecryptMessage(ciphertext, nonce, senderPubKey)` - NaCl box encryption via Solana keys
- `SetDatabase(db)` - Enable optional transaction logging

**Key Derivation** (`derivation.go`) - BIP39/BIP44/SLIP-0010:
- `GenerateMnemonic()` - 24-word BIP39 mnemonic
- `ValidateMnemonic(mnemonic)` - BIP39 validation
- `DeriveKeysFromMnemonic(mnemonic, accountIndex)` - Derive both chains from one seed
- ETH path: `m/44'/60'/0'/0/{index}` (BIP44)
- SOL path: `m/44'/501'/{index}'/0'` (SLIP-0010)

**Types** (`types.go`) - Shared result types:
- `Chain` - `"ethereum"` or `"solana"` with `IsValid()` method
- `WalletInfo`, `SignedMessage`, `TransactionResult`, `SwapResult`, `ContractCallResult`, `BalanceResult`

### Ethereum Implementation (`ethereum_*.go`)

| File | Purpose |
|------|---------|
| `ethereum_wallet.go` | ECDSA wallet creation, address derivation |
| `ethereum_sign.go` | EIP-191 personal_sign, raw hash signing |
| `ethereum_verify.go` | Signature verification via public key recovery |
| `ethereum_balance.go` | Native ETH and ERC20 token balance queries |
| `ethereum_send.go` | Native ETH and ERC20 token transfers |
| `ethereum_swap.go` | Token swaps via 0x API |
| `ethereum_contract.go` | Smart contract calls (read-only and write, simplified ABI encoding) |
| `ethereum_constants.go` | ERC20 ABI definition |

### Solana Implementation (`solana_*.go`)

| File | Purpose |
|------|---------|
| `solana_wallet.go` | Ed25519 wallet creation from Base58 keypair |
| `solana_sign.go` | Ed25519 message signing |
| `solana_verify.go` | Signature verification |
| `solana_balance.go` | Native SOL and SPL token balance queries |
| `solana_send.go` | SOL and SPL transfers with optional memo |
| `solana_swap.go` | Token swaps via Jupiter API (v6) |
| `solana_contract.go` | Program instruction building |
| `solana_constants.go` | Well-known addresses (USDC, NativeSOL, MemoProgram) |

### NaCl Encryption (`nacl.go`)

X25519 Diffie-Hellman + XSalsa20-Poly1305 authenticated encryption:
- `Ed25519PrivateKeyToX25519(privKey)` - Key conversion (SHA-512 + clamp)
- `Ed25519PublicKeyToX25519(pubKey)` - Edwards-to-Montgomery point conversion
- `NaClBoxSeal(plaintext, recipientPub, senderPriv)` - Encrypt with random nonce
- `NaClBoxOpen(ciphertext, nonce, senderPub, recipientPriv)` - Decrypt

Used by the `agent_net` library for E2E encrypted direct messaging between agents.

### Transaction Logging (`transaction_log.go`)

Optional SQLite/PostgreSQL logging of all wallet operations:
- `NewTransactionLogger(db)` - No-op if db is nil
- `InitSchema(ctx)` - Create `wallet_transaction_log` table
- `Log(ctx, tx)` / `UpdateStatus(ctx, hash, status, error)` - Record and update
- `GetRecent(ctx, limit)` / `GetByHash(ctx, hash)` / `GetByChain(ctx, chain, limit)` - Query

## Environment Variables

```bash
# Ethereum
WALLET_ETH_PRIVATE_KEY=0x...     # Hex private key
WALLET_ETH_RPC_URL=https://...   # RPC endpoint

# Solana
WALLET_SOL_PRIVATE_KEY=...       # Base58 keypair (64 bytes)
WALLET_SOL_RPC_URL=https://...   # RPC endpoint

# Key derivation (alternative to explicit keys)
WALLET_SEED_PHRASE="24 word mnemonic..."
```

## Key Dependencies

- `github.com/ethereum/go-ethereum` - Ethereum client, ECDSA, smart contracts
- `github.com/gagliardetto/solana-go` - Solana RPC client, transaction building
- `github.com/tyler-smith/go-bip39` - BIP39 mnemonic generation/validation
- `github.com/btcsuite/btcd` - BIP44 HD key derivation
- `filippo.io/edwards25519` - Ed25519-to-X25519 public key conversion
- `golang.org/x/crypto` - NaCl box encryption

## Design Patterns

1. **Private key encapsulation**: Keys loaded from env/config, kept in memory, never logged or exported
2. **Unified multi-chain interface**: Single `Wallet` type wraps both `EthereumWallet` and `SolanaWallet`
3. **Human-readable amounts**: Amounts as strings (e.g., "1.5") converted to chain units (wei, lamports) internally
4. **Optional logging**: Transaction logging is no-op when no database is configured
5. **Case-insensitive chains**: Chain strings normalized to lowercase

## Common Pitfalls

- **Amount encoding**: Always use human-readable strings ("1.5"), not raw units. The library handles wei/lamport conversion.
- **Solana keypair format**: 64 bytes Base58-encoded (32-byte seed + 32-byte public key), not just the seed.
- **NaCl key conversion**: Ed25519 keys must be converted to X25519 before NaCl box operations. Use the provided conversion functions.
- **ERC20 method signatures**: Smart contract calls require full method signature (e.g., `"transfer(address,uint256)"`), not just the name.
