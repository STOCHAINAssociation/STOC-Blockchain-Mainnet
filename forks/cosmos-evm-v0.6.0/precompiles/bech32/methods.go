package bech32

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	cmn "github.com/cosmos/evm/precompiles/common"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// HexToBech32Method defines the ABI method name to convert a EIP-55
	// hex formatted address to bech32 address string.
	HexToBech32Method = "hexToBech32"
	// Bech32ToHexMethod defines the ABI method name to convert a bech32
	// formatted address string to an EIP-55 address.
	Bech32ToHexMethod = "bech32ToHex"
)

// HexToBech32 converts a hex address to its corresponding Bech32 format. The Human Readable Prefix
// (HRP) must be provided in the arguments. This function fails if the address is invalid or if the
// bech32 conversion fails.
func (p Precompile) HexToBech32(
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf(cmn.ErrInvalidNumberOfArgs, 2, len(args))
	}

	address, ok := args[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("invalid hex address")
	}

	cfg := sdk.GetConfig()

	prefix, _ := args[1].(string)
	if strings.TrimSpace(prefix) == "" {
		return nil, fmt.Errorf(
			"invalid bech32 human readable prefix (HRP). Please provide a either an account, validator or consensus address prefix (eg: %s, %s, %s)",
			cfg.GetBech32AccountAddrPrefix(), cfg.GetBech32ValidatorAddrPrefix(), cfg.GetBech32ConsensusAddrPrefix(),
		)
	}

	// NOTE: safety check, should not happen given that the address is 20 bytes.
	if err := sdk.VerifyAddressFormat(address.Bytes()); err != nil {
		return nil, err
	}

	bech32Str, err := sdk.Bech32ifyAddressBytes(prefix, address.Bytes())
	if err != nil {
		return nil, err
	}

	return method.Outputs.Pack(bech32Str)
}

// Bech32ToHex converts a bech32 address to its corresponding EIP-55 hex format. The Human Readable Prefix
// (HRP) must be provided in the arguments. This function fails if the address is invalid or if the
// bech32 conversion fails.
//
// SA-L11 audit-2026-05-29: HRP is whitelisted against the chain's account /
// validator / consensus prefixes. Without the whitelist, an attacker could
// pass `osmo1...`, `cosmos1...`, etc. and obtain the underlying 20-byte
// payload — a downstream contract that "trusts" the returned hex as a local
// STOC address would then be fooled by what is actually an address from a
// different chain (cross-chain address-spoof / phishing).
func (p Precompile) Bech32ToHex(
	method *abi.Method,
	args []interface{},
) ([]byte, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf(cmn.ErrInvalidNumberOfArgs, 1, len(args))
	}

	address, ok := args[0].(string)
	if !ok || address == "" {
		return nil, fmt.Errorf("invalid bech32 address: %v", args[0])
	}

	bech32Prefix := strings.SplitN(address, "1", 2)[0]
	if bech32Prefix == address {
		return nil, fmt.Errorf("invalid bech32 address: %s", address)
	}

	cfg := sdk.GetConfig()
	allowedPrefixes := map[string]struct{}{
		cfg.GetBech32AccountAddrPrefix():   {},
		cfg.GetBech32ValidatorAddrPrefix(): {},
		cfg.GetBech32ConsensusAddrPrefix(): {},
	}
	if _, ok := allowedPrefixes[bech32Prefix]; !ok {
		return nil, fmt.Errorf(
			"bech32 HRP %q not allowed; expected one of [%s, %s, %s] (cross-chain address spoofing prevented)",
			bech32Prefix,
			cfg.GetBech32AccountAddrPrefix(), cfg.GetBech32ValidatorAddrPrefix(), cfg.GetBech32ConsensusAddrPrefix(),
		)
	}

	addressBz, err := sdk.GetFromBech32(address, bech32Prefix)
	if err != nil {
		return nil, err
	}

	if err := sdk.VerifyAddressFormat(addressBz); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(common.BytesToAddress(addressBz))
}
