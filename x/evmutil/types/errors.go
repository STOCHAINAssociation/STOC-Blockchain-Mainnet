package types

import (
	errorsmod "cosmossdk.io/errors"
)

var (
	// ErrInvalidDenom is returned when an unsupported denom is used
	ErrInvalidDenom = errorsmod.Register(ModuleName, 2, "invalid denom")

	// ErrConversionOverflow is returned when a conversion would overflow
	ErrConversionOverflow = errorsmod.Register(ModuleName, 3, "conversion overflow")

	// ErrInsufficientBalance is returned when an account has insufficient balance
	ErrInsufficientBalance = errorsmod.Register(ModuleName, 4, "insufficient balance")
)
