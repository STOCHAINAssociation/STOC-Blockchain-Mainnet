package types_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

func TestMsgCreateToken_ValidateBasic(t *testing.T) {
	// Set DefaultBondDenom to "ustoc" so IsNativeDenom doesn't panic via GetEvmDenom()
	oldBondDenom := sdk.DefaultBondDenom
	sdk.DefaultBondDenom = "ustoc"
	defer func() { sdk.DefaultBondDenom = oldBondDenom }()

	creator := sdk.AccAddress([]byte("creator_address_123")).String()
	creator2 := sdk.AccAddress([]byte("creator_address_456")).String()

	tests := []struct {
		name    string
		msg     types.MsgCreateToken
		wantErr bool
	}{
		{
			name: "valid message",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
				Distributions: []types.WalletDistribution{
					{Address: creator, Percent: 100},
				},
				Tax: types.TokenTax{
					Percent:          math.LegacyNewDecWithPrec(1, 1),
					RecipientAddress: creator,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid creator address",
			msg: types.MsgCreateToken{
				Creator:       "invalid_address",
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "empty name",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "name too long (65 chars)",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          strings.Repeat("A", 65),
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "empty symbol",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "invalid symbol starts with number",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "1MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "invalid logo no https",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "http://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "negative initial supply",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(-1),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "total supply less than initial supply",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(1000),
				TotalSupply:   math.NewInt(500),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "zero total supply non-unlimited (dead token)",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.ZeroInt(),
				TotalSupply:   math.ZeroInt(),
				Unlimited:     false,
			},
			wantErr: true,
		},
		{
			name: "distribution percentages do not sum to 100",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
				Distributions: []types.WalletDistribution{
					{Address: creator, Percent: 50},
					{Address: creator2, Percent: 30},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate distribution address",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
				Distributions: []types.WalletDistribution{
					{Address: creator, Percent: 50},
					{Address: creator, Percent: 50},
				},
			},
			wantErr: true,
		},
		{
			name: "tax exceeds 50%",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
				Tax: types.TokenTax{
					Percent:          math.LegacyNewDecWithPrec(6, 1), // 0.6 = 60%
					RecipientAddress: creator,
				},
			},
			wantErr: true,
		},
		{
			name: "tax positive but no recipient",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
				Tax: types.TokenTax{
					Percent:          math.LegacyNewDecWithPrec(1, 1), // 10%
					RecipientAddress: "",
				},
			},
			wantErr: true,
		},
		{
			name: "decimals exceeds 18",
			msg: types.MsgCreateToken{
				Creator:       creator,
				Name:          "My Token",
				Symbol:        "MYT",
				Decimals:      19,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(500),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// L17 fix: distribution percent overflow check
func TestMsgCreateToken_DistributionOverflow(t *testing.T) {
	oldBondDenom := sdk.DefaultBondDenom
	sdk.DefaultBondDenom = "ustoc"
	defer func() { sdk.DefaultBondDenom = oldBondDenom }()

	creator := sdk.AccAddress([]byte("creator_address_123")).String()
	creator2 := sdk.AccAddress([]byte("creator_address_456")).String()

	tests := []struct {
		name    string
		dists   []types.WalletDistribution
		wantErr bool
	}{
		{
			name: "valid 50+50=100",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 50},
				{Address: creator2, Percent: 50},
			},
			wantErr: false,
		},
		{
			name: "valid 33+33+34=100",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 33},
				{Address: creator2, Percent: 33},
				{Address: sdk.AccAddress([]byte("creator_address_789")).String(), Percent: 34},
			},
			wantErr: false,
		},
		{
			name: "overflow: 100+100 > 100",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 100},
				{Address: creator2, Percent: 100},
			},
			wantErr: true,
		},
		{
			name: "overflow: 60+60 > 100",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 60},
				{Address: creator2, Percent: 60},
			},
			wantErr: true,
		},
		{
			name: "not 100: 30+30=60",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 30},
				{Address: creator2, Percent: 30},
			},
			wantErr: true,
		},
		{
			name: "zero percent rejected",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 0},
			},
			wantErr: true,
		},
		{
			name: "single 100%",
			dists: []types.WalletDistribution{
				{Address: creator, Percent: 100},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := types.MsgCreateToken{
				Creator:       creator,
				Name:          "Test Token",
				Symbol:        "TST",
				Decimals:      6,
				Logo:          "https://example.com/logo.png",
				InitialSupply: math.NewInt(1000),
				TotalSupply:   math.NewInt(1000),
				Unlimited:     false,
				Distributions: tc.dists,
			}
			err := msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err, "expected error for: %s", tc.name)
			} else {
				require.NoError(t, err, "unexpected error for: %s", tc.name)
			}
		})
	}
}

func TestMsgMintTokens_ValidateBasic(t *testing.T) {
	creator := sdk.AccAddress([]byte("creator_address_123")).String()

	tests := []struct {
		name    string
		msg     types.MsgMintTokens
		wantErr bool
	}{
		{
			name: "valid message",
			msg: types.MsgMintTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Amount:  math.NewInt(1000),
			},
			wantErr: false,
		},
		{
			name: "empty symbol",
			msg: types.MsgMintTokens{
				Creator: creator,
				Symbol:  "",
				Amount:  math.NewInt(1000),
			},
			wantErr: true,
		},
		{
			name: "zero amount",
			msg: types.MsgMintTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Amount:  math.ZeroInt(),
			},
			wantErr: true,
		},
		{
			name: "negative amount",
			msg: types.MsgMintTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Amount:  math.NewInt(-100),
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgReleaseTokens_ValidateBasic(t *testing.T) {
	creator := sdk.AccAddress([]byte("creator_address_123")).String()
	recipient := sdk.AccAddress([]byte("recipient_addr_1234")).String()
	recipient2 := sdk.AccAddress([]byte("recipient_addr_5678")).String()

	tests := []struct {
		name    string
		msg     types.MsgReleaseTokens
		wantErr bool
	}{
		{
			name: "valid single recipient",
			msg: types.MsgReleaseTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Distributions: []types.ReleaseRecipient{
					{Address: recipient, Amount: math.NewInt(100)},
				},
			},
			wantErr: false,
		},
		{
			name: "valid multi-recipient",
			msg: types.MsgReleaseTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Distributions: []types.ReleaseRecipient{
					{Address: recipient, Amount: math.NewInt(100)},
					{Address: recipient2, Amount: math.NewInt(50)},
				},
			},
			wantErr: false,
		},
		{
			name: "empty symbol",
			msg: types.MsgReleaseTokens{
				Creator: creator,
				Symbol:  "",
				Distributions: []types.ReleaseRecipient{
					{Address: recipient, Amount: math.NewInt(100)},
				},
			},
			wantErr: true,
		},
		{
			name: "empty distributions",
			msg: types.MsgReleaseTokens{
				Creator:       creator,
				Symbol:        "MYT_0",
				Distributions: []types.ReleaseRecipient{},
			},
			wantErr: true,
		},
		{
			name: "zero amount entry",
			msg: types.MsgReleaseTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Distributions: []types.ReleaseRecipient{
					{Address: recipient, Amount: math.ZeroInt()},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid recipient address",
			msg: types.MsgReleaseTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Distributions: []types.ReleaseRecipient{
					{Address: "invalid_recipient", Amount: math.NewInt(100)},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate recipient address",
			msg: types.MsgReleaseTokens{
				Creator: creator,
				Symbol:  "MYT_0",
				Distributions: []types.ReleaseRecipient{
					{Address: recipient, Amount: math.NewInt(100)},
					{Address: recipient, Amount: math.NewInt(50)},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgBurnToken_ValidateBasic(t *testing.T) {
	creator := sdk.AccAddress([]byte("creator_address_123")).String()

	tests := []struct {
		name    string
		msg     types.MsgBurnToken
		wantErr bool
	}{
		{
			name: "valid message burn specific amount",
			msg: types.MsgBurnToken{
				Creator: creator,
				Denom:   "MYT_0",
				Amount:  math.NewInt(100),
				BurnAll: false,
			},
			wantErr: false,
		},
		{
			name: "valid message burn all",
			msg: types.MsgBurnToken{
				Creator: creator,
				Denom:   "MYT_0",
				BurnAll: true,
			},
			wantErr: false,
		},
		{
			name: "burn all with amount set",
			msg: types.MsgBurnToken{
				Creator: creator,
				Denom:   "MYT_0",
				Amount:  math.NewInt(100),
				BurnAll: true,
			},
			wantErr: true,
		},
		{
			name: "empty denom",
			msg: types.MsgBurnToken{
				Creator: creator,
				Denom:   "",
				Amount:  math.NewInt(100),
				BurnAll: false,
			},
			wantErr: true,
		},
		{
			name: "burn all false zero amount",
			msg: types.MsgBurnToken{
				Creator: creator,
				Denom:   "MYT_0",
				Amount:  math.ZeroInt(),
				BurnAll: false,
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
