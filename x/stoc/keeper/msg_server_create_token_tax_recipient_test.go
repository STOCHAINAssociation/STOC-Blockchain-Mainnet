package keeper_test

import (
	"errors"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

// v5.0.3 audit round 1: MsgCreateToken only accepts a tax recipient that is
// an existing plain user wallet, because the tax PostHandler is fail-closed
// and a recipient that cannot receive custom tokens would freeze the token.

func taxedCreateMsg(creator, recipient string, percent math.LegacyDec) *types.MsgCreateToken {
	return &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Taxed",
		Symbol:        "TXR",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Tax:           types.TokenTax{Percent: percent, RecipientAddress: recipient},
	}
}

func TestCreateToken_TaxRecipient_Validation(t *testing.T) {
	creator := sdk.AccAddress([]byte("creator_address_123"))
	recipient := sdk.AccAddress([]byte("tax_recipient_v503__"))
	tenPct := math.LegacyNewDecWithPrec(1, 1)

	cases := []struct {
		name    string
		setup   func(acc *MockAccountKeeper, bank *MockBankKeeper)
		percent math.LegacyDec
		errMsg  string
	}{
		{
			name:    "existing plain wallet accepted",
			setup:   func(acc *MockAccountKeeper, _ *MockBankKeeper) { acc.AddWallet(recipient) },
			percent: tenPct,
		},
		{
			name:    "existing wallet with a secp256k1 key accepted",
			percent: tenPct,
			setup: func(acc *MockAccountKeeper, _ *MockBankKeeper) {
				base := authtypes.NewBaseAccountWithAddress(recipient)
				require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
				acc.Accounts[recipient.String()] = base
			},
		},
		{
			name:    "non-existent recipient rejected",
			setup:   func(*MockAccountKeeper, *MockBankKeeper) {},
			percent: tenPct,
			errMsg:  "does not exist on chain",
		},
		{
			name:    "non-existent recipient rejected even with zero tax",
			setup:   func(*MockAccountKeeper, *MockBankKeeper) {},
			percent: math.LegacyZeroDec(),
			errMsg:  "does not exist on chain",
		},
		{
			name:    "vesting (permanently locked) recipient rejected",
			percent: tenPct,
			setup: func(acc *MockAccountKeeper, _ *MockBankKeeper) {
				locked, err := vestingtypes.NewPermanentLockedAccount(
					authtypes.NewBaseAccountWithAddress(recipient), sdk.NewCoins(sdk.NewInt64Coin("ustoc", 1)))
				require.NoError(t, err)
				acc.Accounts[recipient.String()] = locked
			},
			errMsg: "must be a plain user wallet",
		},
		{
			name:    "module account recipient rejected",
			percent: tenPct,
			setup: func(acc *MockAccountKeeper, _ *MockBankKeeper) {
				acc.Accounts[recipient.String()] = authtypes.NewModuleAccount(
					authtypes.NewBaseAccountWithAddress(recipient), "somemodule")
			},
			errMsg: "must be a plain user wallet",
		},
		{
			name:    "module-credential (group policy shape) recipient rejected",
			percent: tenPct,
			setup: func(acc *MockAccountKeeper, _ *MockBankKeeper) {
				cred, err := authtypes.NewModuleCredential("group", []byte{0x01}, []byte{0, 0, 0, 0, 0, 0, 0, 1})
				require.NoError(t, err)
				base, err := authtypes.NewBaseAccountWithPubKey(cred)
				require.NoError(t, err)
				// register under the test recipient key so the lookup finds it
				acc.Accounts[recipient.String()] = base
			},
			errMsg: "module-derived account",
		},
		{
			name:    "bank-blocked recipient rejected",
			percent: tenPct,
			setup: func(acc *MockAccountKeeper, bank *MockBankKeeper) {
				acc.AddWallet(recipient)
				bank.Blocked = map[string]bool{recipient.String(): true}
			},
			errMsg: "blocked address",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, ms, ctx, mockBank, mockAcc := setupMsgServerWithMocks(t)
			tc.setup(mockAcc, mockBank)

			_, err := ms.CreateToken(ctx, taxedCreateMsg(creator.String(), recipient.String(), tc.percent))
			if tc.errMsg == "" {
				require.NoError(t, err)
				_, found := k.GetToken(ctx, "TXR_0")
				require.True(t, found)
				return
			}
			require.Error(t, err)
			require.ErrorIs(t, err, types.ErrInvalidTaxRecipient)
			require.Contains(t, err.Error(), tc.errMsg)
			_, found := k.GetToken(ctx, "TXR_0")
			require.False(t, found, "token must not be created")
		})
	}
}

// The app installs an extra check (module account addresses, group
// policies, EVM contracts). Its rejection must fail MsgCreateToken.
func TestCreateToken_TaxRecipient_AppCheck(t *testing.T) {
	k, ms, ctx, _, mockAcc := setupMsgServerWithMocks(t)
	creator := sdk.AccAddress([]byte("creator_address_123"))
	recipient := sdk.AccAddress([]byte("tax_recipient_v503__"))
	mockAcc.AddWallet(recipient)

	var seen sdk.AccAddress
	k.SetTaxRecipientCheck(func(_ sdk.Context, addr sdk.AccAddress) error {
		seen = addr
		return errors.New("EVM contract")
	})

	_, err := ms.CreateToken(ctx, taxedCreateMsg(creator.String(), recipient.String(), math.LegacyNewDecWithPrec(1, 1)))
	require.ErrorIs(t, err, types.ErrInvalidTaxRecipient)
	require.Contains(t, err.Error(), "EVM contract")
	require.Equal(t, recipient, seen)

	// A token without tax and without a recipient never consults the check.
	seen = nil
	msg := taxedCreateMsg(creator.String(), "", math.LegacyZeroDec())
	_, err = ms.CreateToken(ctx, msg)
	require.NoError(t, err)
	require.Nil(t, seen)
}
