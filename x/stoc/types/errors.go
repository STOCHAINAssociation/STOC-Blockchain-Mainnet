package types

// DONTCOVER

import (
	sdkerrors "cosmossdk.io/errors"
)

// x/stoc module sentinel errors
var (
	ErrInvalidSigner = sdkerrors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrSample        = sdkerrors.Register(ModuleName, 1101, "sample error")

	//Token errors
	ErrTokenExists = sdkerrors.Register(ModuleName, 1500, "token already exists")
	ErrInvalidToken = sdkerrors.Register(ModuleName, 1501, "invalid token")
)
