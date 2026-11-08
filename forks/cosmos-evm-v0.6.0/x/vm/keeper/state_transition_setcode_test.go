package keeper

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

// TestValidateAuthorizationRejectsChainIDZero pins the STOChain EIP-7702
// deviation (fix17 A15-CRYPTO-H2, documented fix19 A16-CRYPTO-L1): a
// ChainID=0 "any chain" wildcard authorization must be rejected BEFORE
// authority recovery and BEFORE access-list warming. The nil StateDB below
// is intentional — if a future upstream re-sync moves the reject after
// state.AddAddressToAccessList, this test panics on the nil state instead
// of silently re-opening the coin-type-118 cross-chain replay vector.
func TestValidateAuthorizationRejectsChainIDZero(t *testing.T) {
	k := &Keeper{}
	auth := &ethtypes.SetCodeAuthorization{
		ChainID: *uint256.NewInt(0),
		Nonce:   1,
	}

	_, err := k.validateAuthorization(auth, nil, big.NewInt(1306))
	if err == nil {
		t.Fatal("ChainID=0 authorization must be rejected (A15-CRYPTO-H2)")
	}
	if !errors.Is(err, core.ErrAuthorizationWrongChainID) {
		t.Fatalf("expected ErrAuthorizationWrongChainID, got: %v", err)
	}
}

// TestValidateAuthorizationWrongChainIDStillRejected sanity-pins the
// standard (non-deviation) chain-id-mismatch path alongside the wildcard
// reject above.
func TestValidateAuthorizationWrongChainIDStillRejected(t *testing.T) {
	k := &Keeper{}
	auth := &ethtypes.SetCodeAuthorization{
		ChainID: *uint256.NewInt(999),
		Nonce:   1,
	}

	_, err := k.validateAuthorization(auth, nil, big.NewInt(1306))
	if !errors.Is(err, core.ErrAuthorizationWrongChainID) {
		t.Fatalf("expected ErrAuthorizationWrongChainID for mismatched chain id, got: %v", err)
	}
}
