package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Run modes.
const (
	ModeOnce     = "once"
	ModeSchedule = "schedule"
)

// Environment variable names for the documented config keys.
const (
	envMode                 = "POLYMARKET_NO_BUYER_MODE"
	envInterval             = "POLYMARKET_NO_BUYER_INTERVAL"
	envDryRun               = "POLYMARKET_NO_BUYER_DRY_RUN"
	envMinNoMidpoint        = "POLYMARKET_NO_BUYER_MIN_NO_MIDPOINT"
	envMaxNoMidpoint        = "POLYMARKET_NO_BUYER_MAX_NO_MIDPOINT"
	envTargetExposurePct    = "POLYMARKET_NO_BUYER_TARGET_EXPOSURE_PCT"
	envMinLiquidityUSD      = "POLYMARKET_NO_BUYER_MIN_LIQUIDITY_USD"
	envMinHoursToClose      = "POLYMARKET_NO_BUYER_MIN_HOURS_TO_CLOSE"
	envMaxHoursToClose      = "POLYMARKET_NO_BUYER_MAX_HOURS_TO_CLOSE"
	envOrderExpiryBefore    = "POLYMARKET_NO_BUYER_ORDER_EXPIRY_BEFORE_CLOSE"
	envUSDCTokenAddress     = "POLYMARKET_NO_BUYER_USDC_TOKEN_ADDRESS"
	envMinOrderSizeFallback = "POLYMARKET_NO_BUYER_MIN_ORDER_SIZE_FALLBACK"
	envVerbose              = "POLYMARKET_NO_BUYER_VERBOSE"
)

// Documented PRD defaults.
const (
	defaultMode              = ModeOnce
	defaultInterval          = 6 * time.Hour
	defaultDryRun            = false
	defaultMinNoMidpoint     = 0.89
	defaultMaxNoMidpoint     = 0.99
	defaultTargetExposurePct = 0.01
	defaultMinLiquidityUSD   = 5000.0
	defaultMinHoursToClose   = 48 * time.Hour
	defaultMaxHoursToClose   = 336 * time.Hour
	defaultOrderExpiryBefore = 24 * time.Hour
)

// defaultUSDCTokenAddress is the collateral token whose wallet balance seeds the
// per-run spendable budget. It MUST be the token CLOB V2 orders actually settle in
// — pUSD (the same token allowances.go approves to the exchanges) — not USDC.e.
// Reading USDC.e here makes the app size orders against funds the CLOB will not
// accept, which the exchange rejects with "not enough balance / allowance".
var defaultUSDCTokenAddress = polymarket.PUSDAddress

// Config is the resolved effective configuration for a run, exposed as a single
// struct for downstream rungs. Values are resolved with precedence:
// explicit CLI flag > environment variable > documented PRD default.
type Config struct {
	Mode                   string
	Interval               time.Duration
	DryRun                 bool
	MinNoMidpoint          float64
	MaxNoMidpoint          float64
	TargetExposurePct      float64
	MinLiquidityUSD        float64
	MinHoursToClose        time.Duration
	MaxHoursToClose        time.Duration
	OrderExpiryBeforeClose time.Duration
	USDCTokenAddress       string

	// MinOrderSizeFallback is an optional test-only fallback. It is only honored
	// when MinOrderSizeFallbackSet is true; production leaves it unset and fails
	// closed rather than guessing a venue minimum order size.
	MinOrderSizeFallback    float64
	MinOrderSizeFallbackSet bool

	// Verbose controls human-readable output detail: when false the app prints
	// only the actions it takes; when true it also prints skipped markets and the
	// reasons they were skipped.
	Verbose bool
}

// defaultConfig returns a Config populated with the documented PRD defaults.
func defaultConfig() Config {
	return Config{
		Mode:                   defaultMode,
		Interval:               defaultInterval,
		DryRun:                 defaultDryRun,
		MinNoMidpoint:          defaultMinNoMidpoint,
		MaxNoMidpoint:          defaultMaxNoMidpoint,
		TargetExposurePct:      defaultTargetExposurePct,
		MinLiquidityUSD:        defaultMinLiquidityUSD,
		MinHoursToClose:        defaultMinHoursToClose,
		MaxHoursToClose:        defaultMaxHoursToClose,
		OrderExpiryBeforeClose: defaultOrderExpiryBefore,
		USDCTokenAddress:       defaultUSDCTokenAddress,
	}
}

// rawFlags captures the parsed CLI flags before precedence is applied. The
// pointers let LoadConfig detect which flags were explicitly set.
type rawFlags struct {
	once      bool
	schedule  bool
	interval  string
	dryRun    bool
	verbose   bool
	minLiquid float64
	setFlags  map[string]bool
}

// LoadConfig resolves the effective configuration from defaults, environment
// variables, and the provided CLI arguments, applying flag > env > default
// precedence. It returns a usage error for malformed values or contradictory
// flags rather than silently substituting a default.
func LoadConfig(args []string, getenv func(string) string) (*Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := defaultConfig()

	// Layer 1: environment variables override defaults.
	if err := applyEnv(&cfg, getenv); err != nil {
		return nil, err
	}

	// Layer 2: explicit CLI flags override env/defaults.
	rf, err := parseFlags(args)
	if err != nil {
		return nil, err
	}
	if err := applyFlags(&cfg, rf); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func parseFlags(args []string) (rawFlags, error) {
	fs := flag.NewFlagSet("polymarket_no_buyer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var rf rawFlags
	fs.BoolVar(&rf.once, "once", false, "run the pipeline exactly once and exit")
	fs.BoolVar(&rf.schedule, "schedule", false, "run the pipeline repeatedly on an interval")
	fs.StringVar(&rf.interval, "interval", "", "interval between scheduled runs (Go duration, e.g. 6h)")
	fs.BoolVar(&rf.dryRun, "dry-run", false, "log intended actions without mutating account state")
	fs.BoolVar(&rf.verbose, "verbose", false, "also print skipped markets and their skip reasons")
	fs.Float64Var(&rf.minLiquid, "min-liquidity", 0, "minimum market liquidity in USD")

	if err := fs.Parse(args); err != nil {
		return rawFlags{}, err
	}

	rf.setFlags = map[string]bool{}
	fs.Visit(func(f *flag.Flag) { rf.setFlags[f.Name] = true })
	return rf, nil
}

func applyEnv(cfg *Config, getenv func(string) string) error {
	if v := strings.TrimSpace(getenv(envMode)); v != "" {
		mode := strings.ToLower(v)
		if mode != ModeOnce && mode != ModeSchedule {
			return fmt.Errorf("%s must be %q or %q, got %q", envMode, ModeOnce, ModeSchedule, v)
		}
		cfg.Mode = mode
	}
	if v := strings.TrimSpace(getenv(envInterval)); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s: %w", envInterval, err)
		}
		cfg.Interval = d
	}
	if v := strings.TrimSpace(getenv(envDryRun)); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %w", envDryRun, err)
		}
		cfg.DryRun = b
	}
	if v := strings.TrimSpace(getenv(envVerbose)); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %w", envVerbose, err)
		}
		cfg.Verbose = b
	}
	if err := envFloat(getenv, envMinNoMidpoint, &cfg.MinNoMidpoint); err != nil {
		return err
	}
	if err := envFloat(getenv, envMaxNoMidpoint, &cfg.MaxNoMidpoint); err != nil {
		return err
	}
	if err := envFloat(getenv, envTargetExposurePct, &cfg.TargetExposurePct); err != nil {
		return err
	}
	if err := envFloat(getenv, envMinLiquidityUSD, &cfg.MinLiquidityUSD); err != nil {
		return err
	}
	if err := envDuration(getenv, envMinHoursToClose, &cfg.MinHoursToClose); err != nil {
		return err
	}
	if err := envDuration(getenv, envMaxHoursToClose, &cfg.MaxHoursToClose); err != nil {
		return err
	}
	if err := envDuration(getenv, envOrderExpiryBefore, &cfg.OrderExpiryBeforeClose); err != nil {
		return err
	}
	if v := strings.TrimSpace(getenv(envUSDCTokenAddress)); v != "" {
		cfg.USDCTokenAddress = v
	}
	if v := strings.TrimSpace(getenv(envMinOrderSizeFallback)); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", envMinOrderSizeFallback, err)
		}
		cfg.MinOrderSizeFallback = f
		cfg.MinOrderSizeFallbackSet = true
	}
	return nil
}

func applyFlags(cfg *Config, rf rawFlags) error {
	if rf.setFlags["once"] && rf.setFlags["schedule"] {
		return fmt.Errorf("contradictory flags: --once and --schedule cannot both be set")
	}
	if rf.setFlags["once"] {
		cfg.Mode = ModeOnce
	}
	if rf.setFlags["schedule"] {
		cfg.Mode = ModeSchedule
	}
	if rf.setFlags["interval"] {
		d, err := time.ParseDuration(rf.interval)
		if err != nil {
			return fmt.Errorf("--interval: %w", err)
		}
		cfg.Interval = d
	}
	if rf.setFlags["dry-run"] {
		cfg.DryRun = rf.dryRun
	}
	if rf.setFlags["verbose"] {
		cfg.Verbose = rf.verbose
	}
	if rf.setFlags["min-liquidity"] {
		cfg.MinLiquidityUSD = rf.minLiquid
	}
	return nil
}

func (cfg *Config) validate() error {
	if cfg.Mode != ModeOnce && cfg.Mode != ModeSchedule {
		return fmt.Errorf("invalid mode %q (want %q or %q)", cfg.Mode, ModeOnce, ModeSchedule)
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("interval must be positive, got %s", cfg.Interval)
	}
	if cfg.MinNoMidpoint < 0 || cfg.MinNoMidpoint >= 1 {
		return fmt.Errorf("min_no_midpoint must be in [0,1), got %v", cfg.MinNoMidpoint)
	}
	if cfg.MaxNoMidpoint <= cfg.MinNoMidpoint || cfg.MaxNoMidpoint > 1 {
		return fmt.Errorf("max_no_midpoint must be in (min_no_midpoint,1], got %v", cfg.MaxNoMidpoint)
	}
	if cfg.TargetExposurePct <= 0 || cfg.TargetExposurePct > 1 {
		return fmt.Errorf("target_exposure_pct must be in (0,1], got %v", cfg.TargetExposurePct)
	}
	if cfg.MinLiquidityUSD < 0 {
		return fmt.Errorf("min_liquidity_usd must be non-negative, got %v", cfg.MinLiquidityUSD)
	}
	if cfg.MinHoursToClose <= 0 || cfg.MaxHoursToClose <= 0 {
		return fmt.Errorf("close-time window bounds must be positive")
	}
	if cfg.MaxHoursToClose <= cfg.MinHoursToClose {
		return fmt.Errorf("max_hours_to_close (%s) must exceed min_hours_to_close (%s)", cfg.MaxHoursToClose, cfg.MinHoursToClose)
	}
	if cfg.OrderExpiryBeforeClose <= 0 {
		return fmt.Errorf("order_expiry_before_close must be positive, got %s", cfg.OrderExpiryBeforeClose)
	}
	if strings.TrimSpace(cfg.USDCTokenAddress) == "" {
		return fmt.Errorf("usdc_token_address must not be empty")
	}
	return nil
}

func envFloat(getenv func(string) string, key string, dst *float64) error {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*dst = f
	return nil
}

func envDuration(getenv func(string) string, key string, dst *time.Duration) error {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*dst = d
	return nil
}

// fields returns the resolved configuration as an ordered set of structured
// log fields. It excludes secrets; only public, non-sensitive settings appear.
func (cfg *Config) fields() map[string]any {
	f := map[string]any{
		"mode":                      cfg.Mode,
		"interval":                  humanDuration(cfg.Interval),
		"dry_run":                   cfg.DryRun,
		"min_no_midpoint":           cfg.MinNoMidpoint,
		"max_no_midpoint":           cfg.MaxNoMidpoint,
		"target_exposure_pct":       cfg.TargetExposurePct,
		"min_liquidity_usd":         cfg.MinLiquidityUSD,
		"min_hours_to_close":        humanDuration(cfg.MinHoursToClose),
		"max_hours_to_close":        humanDuration(cfg.MaxHoursToClose),
		"order_expiry_before_close": humanDuration(cfg.OrderExpiryBeforeClose),
		"usdc_token_address":        cfg.USDCTokenAddress,
	}
	if cfg.MinOrderSizeFallbackSet {
		f["min_order_size_fallback"] = cfg.MinOrderSizeFallback
	} else {
		f["min_order_size_fallback"] = nil
	}
	return f
}

// humanDuration renders a duration compactly (e.g. 6h, 48h, 336h, 90m) by
// trimming zero-valued trailing components from time.Duration.String().
func humanDuration(d time.Duration) string {
	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	s = strings.TrimSuffix(s, "0m")
	if s == "" {
		return "0s"
	}
	return s
}
