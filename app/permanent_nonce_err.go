package app

import "strings"

// permanentNonceErrSubstrings lists every error-message substring that
// indicates a transaction's nonce is permanently invalid for the current
// sender bucket. When PrepareProposal hits one of these on an EVM tx, the
// cascade-skip drops the entire sender bucket instead of advancing to the
// next nonce of the same account (which would itself fail).
//
// These substrings span three sources:
//   - go-ethereum core: "nonce too high", "nonce too low", "intrinsic gas",
//     "invalid nonce"
//   - cosmos-evm v0.6.0 fork: "nonce is higher than account nonce",
//     "nonce is lower than account nonce" (longer, hand-formatted)
//   - cosmos-sdk core: "invalid sequence"
//
// Kept as a package-level slice so the list is unit-testable.
// permanent_nonce_err_test.go asserts every emitted error wording still
// matches; if an upstream merge changes a string, the test fails in CI
// instead of the cascade-skip silently degrading at runtime.
var permanentNonceErrSubstrings = []string{
	"nonce too high",
	"nonce too low",
	"nonce is higher than account nonce",
	"nonce is lower than account nonce",
	"intrinsic gas",
	"invalid nonce",
	"invalid sequence",
}

// IsPermanentNonceErr reports whether errMsg matches any nonce-class error
// substring that warrants dropping the sender's whole mempool bucket from
// PrepareProposal. Exported for unit tests.
func IsPermanentNonceErr(errMsg string) bool {
	for _, s := range permanentNonceErrSubstrings {
		if strings.Contains(errMsg, s) {
			return true
		}
	}
	return false
}
