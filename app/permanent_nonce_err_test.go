package app

import "testing"

// TestIsPermanentNonceErr asserts every nonce-class error wording currently
// emitted by go-ethereum core, cosmos-evm v0.6.0 fork, and cosmos-sdk core
// still matches IsPermanentNonceErr. If an upstream merge changes a string,
// this test fails at PR time — preventing Bug B cascade-skip from silently
// degrading the way it did between fix7 and fix11.
//
// When adding a new fork or upstream wording, append to BOTH the test cases
// below AND permanentNonceErrSubstrings in permanent_nonce_err.go.
func TestIsPermanentNonceErr(t *testing.T) {
	cases := []struct {
		name    string
		errMsg  string
		isPerma bool
	}{
		// go-ethereum core
		{"geth nonce too high", "tx 0xabc: nonce too high: have 5, want 4", true},
		{"geth nonce too low", "tx 0xabc: nonce too low: have 4, want 5", true},
		{"geth intrinsic gas", "intrinsic gas too low: have 100, want 21000", true},
		{"geth invalid nonce", "invalid nonce", true},

		// cosmos-evm v0.6.0 fork (longer wording)
		{"fork nonce higher", "tx nonce is higher than account nonce: tx 5, account 3", true},
		{"fork nonce lower", "tx nonce is lower than account nonce: tx 2, account 4", true},

		// cosmos-sdk ante
		{"sdk invalid sequence", "account sequence mismatch, expected 5, got 4: invalid sequence", true},

		// negative cases — these must NOT match
		{"insufficient funds", "spendable balance 100 is smaller than 200", false},
		{"out of gas", "out of gas in location: ReadFlat", false},
		{"empty", "", false},
		{"unrelated error", "some other failure", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPermanentNonceErr(tc.errMsg); got != tc.isPerma {
				t.Errorf("IsPermanentNonceErr(%q) = %v, want %v", tc.errMsg, got, tc.isPerma)
			}
		})
	}
}
