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
	ErrTokenExists           = sdkerrors.Register(ModuleName, 1500, "token already exists")
	ErrInvalidToken          = sdkerrors.Register(ModuleName, 1501, "invalid token")
	ErrTokenNotFound         = sdkerrors.Register(ModuleName, 1502, "token not found")
	ErrInsufficientTokens    = sdkerrors.Register(ModuleName, 1503, "insufficient tokens")
	ErrInvalidTokenAmount    = sdkerrors.Register(ModuleName, 1504, "invalid token amount")
	ErrInvalidTokenSymbol    = sdkerrors.Register(ModuleName, 1505, "invalid token symbol")
	ErrUnauthorized          = sdkerrors.Register(ModuleName, 1506, "unauthorized")
	ErrCannotMint            = sdkerrors.Register(ModuleName, 1507, "cannot mint token")
	ErrInvalidAmount         = sdkerrors.Register(ModuleName, 1508, "invalid amount")
	ErrInvalidCreatorAddress = sdkerrors.Register(ModuleName, 1509, "invalid creator address")
)
