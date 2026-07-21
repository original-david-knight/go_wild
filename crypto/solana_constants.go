package gowild_crypto

import "github.com/gagliardetto/solana-go"

// Well-known Solana token addresses
var (
	NativeSOL    = solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112") // Wrapped SOL
	USDC         = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	USDCDecimals = 6
	MemoProgram  = solana.MustPublicKeyFromBase58("MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr")
)
