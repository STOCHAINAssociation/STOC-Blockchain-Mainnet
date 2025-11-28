package types

import (
	"cosmossdk.io/math"
)

const (
	// ModuleName defines the module name
	ModuleName = "evmutil"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// EvmDenom is the EVM token denom with 18 decimals
	EvmDenom = "astoc"

	// CosmosDenom is the Cosmos token denom with 6 decimals
	CosmosDenom = "ustoc"
)

var (
	// ConversionMultiplier is the conversion factor between Cosmos (6 decimals) and EVM (18 decimals)
	// 1 ustoc (10^6) = 10^12 astoc (10^18)
	ConversionMultiplier = math.NewInt(1_000_000_000_000) // 10^12
)
