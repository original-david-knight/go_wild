package tools

// CreateCryptoPaywallInput defines the input for creating a crypto paywall product.
type CreateCryptoPaywallInput struct {
	Title         string `json:"title" description:"Product title displayed to customers" required:"true"`
	Description   string `json:"description" description:"Product description displayed to customers" required:"true"`
	FilePath      string `json:"file_path" description:"Path to the file to sell (must exist in your workspace, e.g. /data/my_ebook.pdf)" required:"true"`
	PriceUSDC     string `json:"price_usdc" description:"Price in USDC as a decimal string (e.g. '4.99')" required:"true"`
	Chain         string `json:"chain" description:"Blockchain for payment" required:"true" enum:"polygon,solana"`
	WalletAddress string `json:"wallet_address" description:"Your wallet address to receive USDC payments" required:"true"`
}
