package gowild_polymarket

const (
	// clobBaseURL is the base URL for the Polymarket CLOB API.
	clobBaseURL = "https://clob.polymarket.com"

	// gammaBaseURL is the base URL for the Polymarket Gamma API (market metadata).
	gammaBaseURL = "https://gamma-api.polymarket.com"

	// dataBaseURL is the base URL for the Polymarket Data API (positions, activity).
	dataBaseURL = "https://data-api.polymarket.com"

	// PolygonRPCURL is the default RPC endpoint used for on-chain settlement transactions.
	// Uses a truly public RPC (polygon-rpc.com is Polymarket-operated and may be disabled).
	PolygonRPCURL = "https://polygon-bor-rpc.publicnode.com"

	// polygonChainID is the chain ID for the Polygon mainnet.
	polygonChainID = 137

	// ctfExchangeAddress is the address of the CLOB V2 CTF Exchange contract on Polygon.
	ctfExchangeAddress = "0xE111180000d2663C0091e4f400237545B87B996B"

	// negRiskCTFExchangeAddress is the address of the CLOB V2 NegRisk CTF Exchange contract on Polygon.
	negRiskCTFExchangeAddress = "0xe2222d279d744050d28e00520010520000310F59"

	// negRiskAdapterAddress is the address of the NegRisk adapter used for settlement/redemption.
	negRiskAdapterAddress = "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296"

	// conditionalTokensAddress is the address of the Conditional Tokens contract on Polygon.
	conditionalTokensAddress = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"

	// PUSDAddress is the pUSD collateral token used by Polymarket CLOB V2 on Polygon.
	PUSDAddress = "0xC011a7E12a19f7B1f670d46F03B03f3342E82DFB"

	// USDCAddress is the bridged USDC (USDC.e) token historically used by Polymarket on Polygon.
	USDCAddress = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"

	// NativeUSDCAddress is the native Circle USDC token on Polygon.
	NativeUSDCAddress = "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359"

	// CollateralOnrampAddress is Polymarket's public USDC -> pUSD on-ramp on Polygon.
	// Its wrap(asset, to, amount) is gated only by onlyUnpaused (no caller role): it
	// pulls `amount` of a supported asset (USDC.e or native USDC) from the caller and
	// mints the same amount of pUSD 1:1, fee-free, to `to`.
	CollateralOnrampAddress = "0x93070a847efEf7F70739046A929D47a521F5B8ee"
)
