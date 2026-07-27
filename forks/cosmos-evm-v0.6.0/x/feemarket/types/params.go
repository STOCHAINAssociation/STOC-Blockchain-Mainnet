package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/params"

	"cosmossdk.io/math"
)

var (
	// DefaultBaseFee for the Cosmos EVM chain
	DefaultBaseFee = math.LegacyNewDec(1_000_000_000)
	// DefaultMinGasMultiplier is 0.5 or 50%
	DefaultMinGasMultiplier = math.LegacyNewDecWithPrec(50, 2)
	// DefaultMinGasPrice is 0 (i.e disabled)
	DefaultMinGasPrice = math.LegacyZeroDec()
	// DefaultEnableHeight is 0 (i.e disabled)
	DefaultEnableHeight = int64(0)
	// DefaultNoBaseFee is false
	DefaultNoBaseFee = false

	ParamsKey = []byte("Params")
)

// NewParams creates a new Params instance
func NewParams(
	noBaseFee bool,
	baseFeeChangeDenom,
	elasticityMultiplier uint32,
	baseFee math.LegacyDec,
	enableHeight int64,
	minGasPrice math.LegacyDec,
	minGasPriceMultiplier math.LegacyDec,
) Params {
	return Params{
		NoBaseFee:                noBaseFee,
		BaseFeeChangeDenominator: baseFeeChangeDenom,
		ElasticityMultiplier:     elasticityMultiplier,
		BaseFee:                  baseFee,
		EnableHeight:             enableHeight,
		MinGasPrice:              minGasPrice,
		MinGasMultiplier:         minGasPriceMultiplier,
	}
}

// DefaultParams returns default evm parameters
func DefaultParams() Params {
	return Params{
		NoBaseFee:                DefaultNoBaseFee,
		BaseFeeChangeDenominator: params.DefaultBaseFeeChangeDenominator,
		ElasticityMultiplier:     params.DefaultElasticityMultiplier,
		BaseFee:                  DefaultBaseFee,
		EnableHeight:             DefaultEnableHeight,
		MinGasPrice:              DefaultMinGasPrice,
		MinGasMultiplier:         DefaultMinGasMultiplier,
	}
}

// SA-H23 audit-2026-05-29: cap BaseFee/MinGasPrice to prevent gov-prop misconfig
// from bricking the chain. 10^18 raw Dec = effective max ~10^6 gwei after wrapper —
// far above any realistic gas price, but bounded.
var MaxFeemarketGasPrice = math.LegacyNewDec(1_000_000_000_000_000_000)

// Validate performs basic validation on fee market parameters.
func (p Params) Validate() error {
	if p.BaseFeeChangeDenominator == 0 {
		return fmt.Errorf("base fee change denominator cannot be 0")
	}

	if p.BaseFee.IsNegative() {
		return fmt.Errorf("initial base fee cannot be negative: %s", p.BaseFee)
	}

	// SA-H23 audit-2026-05-29: cap BaseFee to prevent gov-misconfig EVM halt.
	if p.BaseFee.GT(MaxFeemarketGasPrice) {
		return fmt.Errorf("base fee exceeds max allowed (%s > %s)", p.BaseFee, MaxFeemarketGasPrice)
	}

	if p.EnableHeight < 0 {
		return fmt.Errorf("enable height cannot be negative: %d", p.EnableHeight)
	}

	if p.ElasticityMultiplier == 0 {
		return fmt.Errorf("elasticity multiplier cannot be zero: %d", p.ElasticityMultiplier)
	}

	if err := validateMinGasMultiplier(p.MinGasMultiplier); err != nil {
		return err
	}

	if err := validateMinGasPrice(p.MinGasPrice); err != nil {
		return err
	}

	// SA-H23 audit-2026-05-29: enforce BaseFee >= MinGasPrice invariant.
	// If BaseFee floats below MinGasPrice, the dynamic-fee floor pinches the
	// effective price to MinGasPrice and feemarket signaling drifts.
	if !p.NoBaseFee && p.BaseFee.LT(p.MinGasPrice) {
		return fmt.Errorf("base fee (%s) must be >= min gas price (%s)", p.BaseFee, p.MinGasPrice)
	}

	// FEE1 audit-2026-07-27: when base fee is enabled (NoBaseFee=false), a zero
	// BaseFee removes the sole Cosmos-tx consensus fee floor — DynamicFeeChecker
	// compares feeCap >= BaseFee, so BaseFee=0 admits zero-fee txs (free spam).
	// Reject an explicit zero. This is conditional and cannot break existing
	// configs: NoBaseFee=true legitimately uses BaseFee=0 (v2/v3 upgrade era,
	// skipped by !NoBaseFee); DefaultParams keeps BaseFee=1e9; v5.0.0 sets 0.001.
	// NOTE: the *persistent* floor is MinGasPrice (it clamps the per-block
	// dynamic BaseFee). A code guard on MinGasPrice==0 is intentionally NOT added
	// here because DefaultMinGasPrice=0 with DefaultNoBaseFee=false would reject
	// DefaultParams() and brick fresh genesis. Keep "MinGasPrice > 0 when
	// NoBaseFee=false" as a gov-proposal checklist invariant instead; v5.0.0
	// satisfies it (MinGasPrice=0.001).
	if !p.NoBaseFee && p.BaseFee.IsZero() {
		return fmt.Errorf("base fee cannot be zero when base fee is enabled (NoBaseFee=false): removes the cosmos-tx fee floor")
	}

	return nil
}

func (p Params) IsBaseFeeEnabled(height int64) bool {
	return !p.NoBaseFee && height >= p.EnableHeight
}

func validateMinGasPrice(gasPrice math.LegacyDec) error {
	if gasPrice.IsNil() {
		return fmt.Errorf("invalid parameter: nil")
	}

	if gasPrice.IsNegative() {
		return fmt.Errorf("value cannot be negative: %s", gasPrice)
	}

	// SA-H23 audit-2026-05-29: cap MinGasPrice to prevent gov-misconfig
	// from rejecting all Cosmos txs (chain halt).
	if gasPrice.GT(MaxFeemarketGasPrice) {
		return fmt.Errorf("min gas price exceeds max allowed (%s > %s)", gasPrice, MaxFeemarketGasPrice)
	}

	return nil
}

func validateMinGasMultiplier(multiplier math.LegacyDec) error {
	if multiplier.IsNil() {
		return fmt.Errorf("invalid parameter: nil")
	}

	if multiplier.IsNegative() {
		return fmt.Errorf("value cannot be negative: %s", multiplier)
	}

	if multiplier.GT(math.LegacyOneDec()) {
		return fmt.Errorf("value cannot be greater than 1: %s", multiplier)
	}

	return nil
}
