package ante_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/ante"
	"stoc/x/stoc/types"
)

// SA-AUDIT-2026-06-06 CRIT-1 regression coverage: tax bypass via x/group MsgExec
// wrapping bank.MsgSend on a custom token. The CustomTokenChainOpsRestriction
// must reject any bank.MsgSend / bank.MsgMultiSend wrapped in a
// grouptypes.MsgSubmitProposal at submit time so the proposal is never
// accepted into chain state. Without this gate, an attacker can deposit a
// custom token into a group policy address (BaseAccount, not ModuleAccount —
// bank SendRestriction passes), then drain it tax-free via group proposal
// execution which bypasses the tax PostDecorator.

// registerCustomToken stores a custom token in the keeper so HasToken returns
// true for the given denom. Returns the denom.
func registerCustomToken(t testing.TB, k interface {
	SetToken(sdk.Context, types.Token) error
}, ctx sdk.Context, denom, creator string) {
	t.Helper()
	require.NoError(t, k.SetToken(ctx, types.Token{
		Id:              denom,
		Name:            "Audit Token",
		Symbol:          strings.TrimSuffix(denom, "_0"),
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}))
}

func TestChainOpsRestriction_OuterBankSend_CustomToken_Allowed(t *testing.T) {
	// SA-AUDIT-2026-06-06 CRIT-1: bank.MsgSend at OUTER depth flows through the
	// tax PostDecorator and is allowed by the ante restriction. Verify the
	// fix did not over-block legitimate wallet-to-wallet custom-token transfers.
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, sender.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}
	tx := mockTx{msgs: []sdk.Msg{msg}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err, "outer-depth bank.MsgSend on a custom token must be allowed (tax PostDecorator handles it)")
}

func TestChainOpsRestriction_OuterBankMultiSend_CustomToken_Allowed(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, sender.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	msg := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: sender.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
		}},
		Outputs: []banktypes.Output{{
			Address: recipient.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
		}},
	}
	tx := mockTx{msgs: []sdk.Msg{msg}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err, "outer-depth bank.MsgMultiSend on a custom token must be allowed (tax PostDecorator handles it)")
}

func TestChainOpsRestriction_GroupProposal_InnerBankSendCustomToken_Blocked(t *testing.T) {
	// SA-AUDIT-2026-06-06 CRIT-1 main regression: attacker tries to drain a
	// custom token via group proposal whose inner message is a bank.MsgSend.
	// Must be rejected at submit time so the proposal is never accepted into
	// chain state.
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	proposer := sdk.AccAddress([]byte("proposer____________"))
	policyAddr := sdk.AccAddress([]byte("group_policy_addr___"))
	attacker := sdk.AccAddress([]byte("attacker____________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, proposer.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	innerSend := &banktypes.MsgSend{
		FromAddress: policyAddr.String(),
		ToAddress:   attacker.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1_000_000))),
	}

	prop := &grouptypes.MsgSubmitProposal{
		GroupPolicyAddress: policyAddr.String(),
		Proposers:          []string{proposer.String()},
		Metadata:           "",
		Title:              "bypass",
		Summary:            "drain custom token via group exec",
		Exec:               grouptypes.Exec_EXEC_TRY,
	}
	require.NoError(t, prop.SetMsgs([]sdk.Msg{innerSend}))

	tx := mockTx{msgs: []sdk.Msg{prop}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.Error(t, err, "group MsgSubmitProposal carrying an inner bank.MsgSend on a custom token must be rejected at ante (tax-bypass channel)")
	require.Contains(t, err.Error(), denom)
	require.Contains(t, err.Error(), "bank.MsgSend wrapped in proposal")
}

func TestChainOpsRestriction_GroupProposal_InnerBankMultiSendCustomToken_Blocked(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	proposer := sdk.AccAddress([]byte("proposer____________"))
	policyAddr := sdk.AccAddress([]byte("group_policy_addr___"))
	attacker := sdk.AccAddress([]byte("attacker____________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, proposer.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	innerMultiSend := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: policyAddr.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1_000_000))),
		}},
		Outputs: []banktypes.Output{{
			Address: attacker.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1_000_000))),
		}},
	}

	prop := &grouptypes.MsgSubmitProposal{
		GroupPolicyAddress: policyAddr.String(),
		Proposers:          []string{proposer.String()},
		Metadata:           "",
		Title:              "bypass-multisend",
		Summary:            "drain custom token via group exec multisend",
		Exec:               grouptypes.Exec_EXEC_TRY,
	}
	require.NoError(t, prop.SetMsgs([]sdk.Msg{innerMultiSend}))

	tx := mockTx{msgs: []sdk.Msg{prop}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.Error(t, err, "group MsgSubmitProposal carrying an inner bank.MsgMultiSend on a custom token must be rejected at ante")
	require.Contains(t, err.Error(), denom)
	require.Contains(t, err.Error(), "bank.MsgMultiSend wrapped in proposal")
}

func TestChainOpsRestriction_GroupProposal_InnerBankSendNative_Allowed(t *testing.T) {
	// Native denoms (ustoc/astoc/stoc) inside a group proposal remain allowed.
	// The fix must not regress legitimate group-managed treasury operations on
	// the native token.
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	proposer := sdk.AccAddress([]byte("proposer____________"))
	policyAddr := sdk.AccAddress([]byte("group_policy_addr___"))
	recipient := sdk.AccAddress([]byte("recipient___________"))

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	innerSend := &banktypes.MsgSend{
		FromAddress: policyAddr.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(1_000_000))),
	}

	prop := &grouptypes.MsgSubmitProposal{
		GroupPolicyAddress: policyAddr.String(),
		Proposers:          []string{proposer.String()},
		Metadata:           "",
		Title:              "treasury payout",
		Summary:            "pay grant in ustoc",
		Exec:               grouptypes.Exec_EXEC_TRY,
	}
	require.NoError(t, prop.SetMsgs([]sdk.Msg{innerSend}))

	tx := mockTx{msgs: []sdk.Msg{prop}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err, "group MsgSubmitProposal carrying an inner bank.MsgSend on a native denom must be allowed")
}

func TestChainOpsRestriction_AuthzWrappingBankSendCustomToken_Allowed(t *testing.T) {
	// authz.MsgExec is NOT a tax-bypass wrapper (TaxPostDecorator unwraps it),
	// so authz wrapping bank.MsgSend on a custom token at outer depth must
	// remain allowed.
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	granter := sdk.AccAddress([]byte("granter_____________"))
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, granter.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	innerSend := &banktypes.MsgSend{
		FromAddress: granter.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}
	execMsg := authz.NewMsgExec(grantee, []sdk.Msg{innerSend})

	tx := mockTx{msgs: []sdk.Msg{&execMsg}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err, "authz.MsgExec wrapping a custom-token bank.MsgSend at outer depth must be allowed (tax PostDecorator unwraps authz)")
}

func TestChainOpsRestriction_AuthzWrappingGroupProposalCustomToken_Blocked(t *testing.T) {
	// Nested attack: authz.MsgExec wrapping a group.MsgSubmitProposal whose
	// inner bank.MsgSend touches a custom token. The insideProposalWrapper
	// flag must propagate from the group recursion into any nested authz so
	// the inner bank.MsgSend is rejected.
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	granter := sdk.AccAddress([]byte("granter_____________"))
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	policyAddr := sdk.AccAddress([]byte("group_policy_addr___"))
	attacker := sdk.AccAddress([]byte("attacker____________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, granter.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	innerSend := &banktypes.MsgSend{
		FromAddress: policyAddr.String(),
		ToAddress:   attacker.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1_000_000))),
	}
	prop := &grouptypes.MsgSubmitProposal{
		GroupPolicyAddress: policyAddr.String(),
		Proposers:          []string{granter.String()},
		Metadata:           "",
		Title:              "authz-wrapped group bypass",
		Summary:            "drain via authz→group",
		Exec:               grouptypes.Exec_EXEC_TRY,
	}
	require.NoError(t, prop.SetMsgs([]sdk.Msg{innerSend}))
	execMsg := authz.NewMsgExec(grantee, []sdk.Msg{prop})

	tx := mockTx{msgs: []sdk.Msg{&execMsg}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.Error(t, err, "authz.MsgExec wrapping a group MsgSubmitProposal that carries an inner custom-token bank.MsgSend must be rejected at ante")
	require.Contains(t, err.Error(), denom)
}

// SA-AUDIT-2026-06-07 LOW-1 regression coverage (R2-LOW1): grouptypes.MsgExec
// at outer depth is currently a no-op in the chain-ops restriction (forward-
// defense marker for stale pre-CRIT-1 proposals — see comment in
// custom_token_restriction.go *grouptypes.MsgExec case). The CRIT-1 submit-time
// recursion stops new proposals from entering state, so MsgExec on
// freshly-created proposals can only target proposals whose inner messages
// have already been validated. Pin the no-op behavior so the case stays
// compile-clean and does not regress to a blanket reject (which would break
// legitimate native-denom group flows).
func TestChainOpsRestriction_GroupMsgExec_NoOp(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	executor := sdk.AccAddress([]byte("executor____________"))

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	exec := &grouptypes.MsgExec{
		ProposalId: 1,
		Executor:   executor.String(),
	}

	tx := mockTx{msgs: []sdk.Msg{exec}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err, "LOW-1: grouptypes.MsgExec at outer depth must be a forward-defense no-op (CRIT-1 submit-time recursion handles new proposals)")
}

func TestChainOpsRestriction_GroupProposal_InnerAuthzBankSendCustomToken_Blocked(t *testing.T) {
	// Inverse nesting: group.MsgSubmitProposal whose inner msg is
	// authz.MsgExec wrapping a custom-token bank.MsgSend. The
	// insideProposalWrapper flag set by the outer group must persist through
	// the inner authz recursion.
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	proposer := sdk.AccAddress([]byte("proposer____________"))
	policyAddr := sdk.AccAddress([]byte("group_policy_addr___"))
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	attacker := sdk.AccAddress([]byte("attacker____________"))
	denom := "AUDIT_0"
	registerCustomToken(t, k, ctx, denom, proposer.String())

	decorator := ante.NewCustomTokenChainOpsRestriction(k)

	innerSend := &banktypes.MsgSend{
		FromAddress: policyAddr.String(),
		ToAddress:   attacker.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1_000_000))),
	}
	innerExec := authz.NewMsgExec(grantee, []sdk.Msg{innerSend})

	prop := &grouptypes.MsgSubmitProposal{
		GroupPolicyAddress: policyAddr.String(),
		Proposers:          []string{proposer.String()},
		Metadata:           "",
		Title:              "group→authz bypass",
		Summary:            "drain via group→authz",
		Exec:               grouptypes.Exec_EXEC_TRY,
	}
	require.NoError(t, prop.SetMsgs([]sdk.Msg{&innerExec}))

	tx := mockTx{msgs: []sdk.Msg{prop}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.Error(t, err, "group MsgSubmitProposal whose inner authz.MsgExec wraps a custom-token bank.MsgSend must be rejected at ante (flag persists through authz)")
	require.Contains(t, err.Error(), denom)
}
