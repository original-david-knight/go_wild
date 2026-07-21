package tools

// DescribeTool implements ToolProvider for tool descriptions.
func (w *WalletTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"get_balance": `Check your wallet balance for native tokens (ETH/SOL) or any ERC20/SPL token.

Use this to:
- Check how much ETH or SOL you have
- Check ERC20 token balances (USDC, USDT, etc.)
- Check SPL token balances on Solana
- Verify you have enough funds before a transaction

INPUT:
- chain: "ethereum", "polygon", or "solana"
- token_address: (optional) Contract/mint address for ERC20/SPL tokens. Leave empty for native ETH/SOL.

OUTPUT:
- balance: Human-readable balance (e.g., "1.5")
- balance_raw: Raw balance in smallest units (wei for ETH, lamports for SOL)
- symbol: Token symbol (ETH, SOL, USDC, etc.)
- decimals: Token decimal places

EXAMPLES:
- Check ETH balance: {chain: "ethereum"}
- Check POL balance on Polygon: {chain: "polygon"}
- Check SOL balance: {chain: "solana"}
- Check USDC on Ethereum: {chain: "ethereum", token_address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}
- Check USDT.e on Polygon: {chain: "polygon", token_address: "0xC2132D05D31c914a87C6611C10748AEb04B58e8F"}
- Check USDC on Solana: {chain: "solana", token_address: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"}`,

		"get_wallet_address": `Get the public blockchain address for your wallet.

Use this to:
- Find out your wallet identity ("Who am I?")
- Share your address for receiving funds or verification
- Confirm which wallet is configured

The private key is never exposed - only the public address is returned.

CHAINS: ethereum, solana`,

		"sign_message": `Cryptographically sign a message with your wallet's private key.

Use this to:
- Create verifiable proof that you stated something
- Authenticate your identity to a service
- Create digital seals of authenticity
- Sign attestations or verifications

The signature can be verified by anyone with your public address.

INPUT:
- chain: "ethereum" or "solana"
- message: The text to sign

OUTPUT:
- address: Your public address
- message: The message that was signed
- signature: The cryptographic signature`,

		"send_token": `Send cryptocurrency to another address.

Use this to:
- Transfer ETH/SOL to another wallet
- Send ERC20 tokens (USDC, USDT, etc.) on Ethereum
- Send SPL tokens on Solana
- Make payments with memos (Solana only)
- Pay for premium upgrades with UPGRADE memo

INPUT:
- chain: "ethereum" or "solana"
- to: Destination wallet address
- amount: Human-readable amount (e.g., "1.5" for 1.5 ETH)
- token_address: (optional) Contract address for ERC20/SPL tokens
- memo: (optional, Solana only) Memo text to attach to transaction

OUTPUT:
- transaction_hash: The transaction ID
- explorer_url: Link to view on block explorer
- status: "pending" (check explorer for confirmation)

EXAMPLES:
- Send 0.1 ETH: {chain: "ethereum", to: "0x...", amount: "0.1"}
- Send 100 USDC on ETH: {chain: "ethereum", to: "0x...", amount: "100", token_address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}
- Send 1 SOL: {chain: "solana", to: "...", amount: "1"}
- Send SOL with memo: {chain: "solana", to: "...", amount: "0.0005", memo: "UPGRADE:MyPublicKey..."}`,

		"swap_token": `Swap one token for another using DEX aggregators.

Uses 0x API for Ethereum and Jupiter for Solana to find the best rates.

Use this to:
- Convert ETH to USDC (or vice versa)
- Swap SOL to any SPL token
- Trade between any supported tokens
- Preserve value by moving to stablecoins

INPUT:
- chain: "ethereum" or "solana"
- from_token: Token to sell ("ETH", "SOL", or contract address)
- to_token: Token to buy ("ETH", "SOL", or contract address)
- amount: Amount of from_token to swap
- slippage_bps: (optional) Max slippage in basis points (50 = 0.5%)

OUTPUT:
- transaction_hash: The swap transaction ID
- from_amount: Amount sold
- to_amount: Amount received
- explorer_url: Link to view transaction

EXAMPLES:
- Swap 1 ETH for USDC: {chain: "ethereum", from_token: "ETH", to_token: "0xA0b86991...", amount: "1"}
- Swap 10 SOL for USDC: {chain: "solana", from_token: "SOL", to_token: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", amount: "10"}`,

		"contract_call": `Call a smart contract method - the Swiss Army Knife of blockchain agency.

Use this to:
- Read contract state (balanceOf, owner, etc.)
- Execute contract functions (approve, stake, claim, etc.)
- Interact with DeFi protocols
- Mint NFTs, vote on DAOs, etc.

INPUT:
- chain: "ethereum" or "solana"
  - "ethereum" uses the configured EVM RPC ("WALLET_ETH_RPC_URL") and may point to Polygon
- contract_address: The contract/program address
- method: Function signature (ETH: "transfer(address,uint256)", Solana: instruction data)
- args: Array of arguments matching the method signature
- value: (optional) ETH to send with call
- read_only: (optional) true for view calls that don't modify state

OUTPUT:
- transaction_hash: (for write calls) Transaction ID
- result: (for read calls) Return value
- explorer_url: Link to view transaction

EXAMPLES:
- Check ERC20 balance: {chain: "ethereum", contract_address: "0x...", method: "balanceOf(address)", args: ["0xYourAddress"], read_only: true}
- Approve token spending: {chain: "ethereum", contract_address: "0xToken", method: "approve(address,uint256)", args: ["0xSpender", "1000000000000000000"]}`,

		"encrypt_message": `Encrypt a message for another agent using NaCl box (X25519 + XSalsa20-Poly1305).

Use this to:
- Send end-to-end encrypted direct messages on agent_net
- Encrypt data that only the recipient can read
- Create ciphertext + nonce for the POST /api/v1/messages endpoint

INPUT:
- plaintext: The message text to encrypt
- recipient_public_key: Recipient's Ed25519 public key in base64url (no padding, 43 chars)

OUTPUT:
- ciphertext: Base64url-encoded encrypted data
- nonce: Base64url-encoded 24-byte random nonce

Both ciphertext and nonce must be sent to the server for the recipient to decrypt.`,

		"decrypt_message": `Decrypt a NaCl box message from another agent.

Use this to:
- Read end-to-end encrypted direct messages from agent_net
- Decrypt ciphertext received from GET /api/v1/messages/{pubkey}

INPUT:
- ciphertext: Base64url-encoded ciphertext from the message
- nonce: Base64url-encoded 24-byte nonce from the message
- sender_public_key: Sender's Ed25519 public key in base64url (43 chars)

OUTPUT:
- plaintext: The decrypted message text

Decryption will fail if the ciphertext was not encrypted for your key or was tampered with.`,

		"get_ed25519_public_key": `Get your Ed25519 public key in base64url format.

Use this to:
- Get your agent identity key for the X-Agent-ID header
- Share your public key with other agents for encrypted messaging
- Verify your agent_net identity

OUTPUT:
- public_key: Base64url-encoded Ed25519 public key (no padding, 43 chars)

This is derived from your Solana wallet keypair.`,

		"get_transaction_history": `View your blockchain transaction history.

Use this to:
- Review past transactions
- Check transaction status
- Audit your wallet activity
- Find transaction hashes for reference

All transactions (sends, swaps, contract calls, signatures) are logged.

INPUT:
- limit: (optional) Number of transactions to return (default 20, max 100)
- chain: (optional) Filter by "ethereum" or "solana"

OUTPUT:
- transactions: List of transaction records with:
  - timestamp, chain, type, from_address, to_address
  - amount, token_address (if applicable)
  - transaction_hash, explorer_url (if on-chain)
  - status (pending, confirmed, failed)
  - error (if failed)
  - metadata (additional details like swap tokens, contract method)`,
	}
	return descriptions[name]
}
