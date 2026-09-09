package app

import (
	"strings"
	"testing"
)

// TestICAHostAllowMessagesPinned pins the ICA host AllowMessages allowlist
// set by DefaultGenesis (SA-AUDIT-2026-06-11 fix19 A16-APP-H1).
//
// ibc-go v10 host matches AllowMessages by EXACT type URL and does NOT
// unwrap nested messages — so the only way the fix17 A15-BOUNDARY-H1
// protection (no stoc.* token lifecycle via ICA) breaks at genesis is by
// editing this list. Any change here must update both this test and
// icaHostAllowMessages in app.go, and go through security review.
func TestICAHostAllowMessagesPinned(t *testing.T) {
	want := []string{
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.bank.v1beta1.MsgMultiSend",
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgUndelegate",
		"/cosmos.staking.v1beta1.MsgBeginRedelegate",
		"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation",
		"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
		"/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission",
		"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
		"/cosmos.gov.v1.MsgVote",
		"/cosmos.gov.v1.MsgVoteWeighted",
		"/ibc.applications.transfer.v1.MsgTransfer",
	}

	got := icaHostAllowMessages()

	if len(got) != len(want) {
		t.Fatalf("ICA AllowMessages length = %d, want %d — list is security-pinned, see A16-APP-H1", len(got), len(want))
	}
	for i, url := range want {
		if got[i] != url {
			t.Fatalf("ICA AllowMessages[%d] = %q, want %q — list is security-pinned, see A16-APP-H1", i, got[i], url)
		}
	}
}

// TestICAHostAllowMessagesNoWrappersOrWildcard asserts the allowlist never
// contains a wildcard or a wrapper message that embeds arbitrary inner
// messages (authz/group MsgExec, any MsgSubmitProposal variant). Each of
// those would re-open the stoc.* token-lifecycle bypass closed by fix17
// A15-BOUNDARY-H1, letting a foreign chain mint/release securities tokens
// through ICA.
func TestICAHostAllowMessagesNoWrappersOrWildcard(t *testing.T) {
	for _, url := range icaHostAllowMessages() {
		if url == "*" {
			t.Fatalf("ICA AllowMessages contains wildcard %q — forbidden, see A16-APP-H1", url)
		}
		if strings.HasSuffix(url, ".MsgExec") {
			t.Fatalf("ICA AllowMessages contains wrapper %q — forbidden, see A16-APP-H1", url)
		}
		if strings.Contains(url, "MsgSubmitProposal") {
			t.Fatalf("ICA AllowMessages contains proposal wrapper %q — forbidden, see A16-APP-H1", url)
		}
		if strings.HasPrefix(url, "/stoc.") {
			t.Fatalf("ICA AllowMessages contains stoc.* lifecycle msg %q — forbidden, see A15-BOUNDARY-H1", url)
		}
	}
}
