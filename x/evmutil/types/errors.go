package types

import (
	errorsmod "cosmossdk.io/errors"
)

var (
	// ErrInvalidDenom is returned when an unsupported denom is used
	ErrInvalidDenom = errorsmod.Register(ModuleName, 2, "invalid denom")

	// Error code 3 was ErrConversionOverflow — removed (unused, math.Int uses big.Int internally)

	// ErrInsufficientBalance is returned when an account has insufficient balance
	ErrInsufficientBalance = errorsmod.Register(ModuleName, 4, "insufficient balance")
)
