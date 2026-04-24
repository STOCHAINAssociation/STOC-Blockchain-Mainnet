package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestMsgBurnTokenGetters(t *testing.T) {
	amount := math.NewInt(1000)
	msg := &MsgBurnToken{
		Creator: "stoc1test",
		Amount:  amount,
		Denom:   "ustoc",
		BurnAll: true,
	}

	// Test all getter methods and direct field access
	require.Equal(t, "stoc1test", msg.GetCreator())
	require.Equal(t, amount, msg.Amount) // Direct field access for custom type
	require.Equal(t, "ustoc", msg.GetDenom())
	require.Equal(t, true, msg.GetBurnAll())

	// Test nil message
	var nilMsg *MsgBurnToken
	require.Equal(t, "", nilMsg.GetCreator())
	// Cannot access Amount field directly on nil pointer, skip this test
	require.Equal(t, "", nilMsg.GetDenom())
	require.Equal(t, false, nilMsg.GetBurnAll())
}

func TestMsgMintTokensGetters(t *testing.T) {
	amount := math.NewInt(500)
	msg := &MsgMintTokens{
		Creator: "stoc1test",
		Symbol:  "TEST",
		Amount:  amount,
	}

	// Test all getter methods and direct field access
	require.Equal(t, "stoc1test", msg.GetCreator())
	require.Equal(t, "TEST", msg.GetSymbol())
	require.Equal(t, amount, msg.Amount) // Direct field access for custom type

	// Test nil message
	var nilMsg *MsgMintTokens
	require.Equal(t, "", nilMsg.GetCreator())
	require.Equal(t, "", nilMsg.GetSymbol())
	// Cannot access Amount field directly on nil pointer, skip this test
}

func TestMsgReleaseTokensGetters(t *testing.T) {
	amount := math.NewInt(750)
	msg := &MsgReleaseTokens{
		Creator:   "stoc1test",
		Symbol:    "TEST",
		Amount:    amount,
		Recipient: "stoc1recipient",
	}

	// Test all getter methods and direct field access
	require.Equal(t, "stoc1test", msg.GetCreator())
	require.Equal(t, "TEST", msg.GetSymbol())
	require.Equal(t, amount, msg.Amount) // Direct field access for custom type
	require.Equal(t, "stoc1recipient", msg.GetRecipient())

	// Test nil message
	var nilMsg *MsgReleaseTokens
	require.Equal(t, "", nilMsg.GetCreator())
	require.Equal(t, "", nilMsg.GetSymbol())
	// Cannot access Amount field directly on nil pointer, skip this test
	require.Equal(t, "", nilMsg.GetRecipient())
}

func TestMsgCreateTokenGetters(t *testing.T) {
	initialSupply := math.NewInt(1000000)
	totalSupply := math.NewInt(10000000)
	msg := &MsgCreateToken{
		Creator:       "stoc1test",
		Name:          "Test Token",
		Symbol:        "TEST",
		InitialSupply: initialSupply,
		TotalSupply:   totalSupply,
		Decimals:      6,
		Logo:          "logo.png",
		Unlimited:     false,
	}

	// Test all getter methods and direct field access
	require.Equal(t, "stoc1test", msg.GetCreator())
	require.Equal(t, "Test Token", msg.GetName())
	require.Equal(t, "TEST", msg.GetSymbol())
	require.Equal(t, initialSupply, msg.InitialSupply) // Direct field access for custom type
	require.Equal(t, totalSupply, msg.TotalSupply)     // Direct field access for custom type
	require.Equal(t, uint32(6), msg.GetDecimals())
	require.Equal(t, "logo.png", msg.GetLogo())
	require.Equal(t, false, msg.GetUnlimited())

	// Test nil message
	var nilMsg *MsgCreateToken
	require.Equal(t, "", nilMsg.GetCreator())
	require.Equal(t, "", nilMsg.GetName())
	require.Equal(t, "", nilMsg.GetSymbol())
	// Cannot access InitialSupply and TotalSupply fields directly on nil pointer, skip these tests
	require.Equal(t, uint32(0), nilMsg.GetDecimals())
	require.Equal(t, "", nilMsg.GetLogo())
	require.Equal(t, false, nilMsg.GetUnlimited())
}
