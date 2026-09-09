package app

import "strings"

// permanentNonceErrSubstrings lists every error-message substring that
// indicates a transaction's nonce is permanently invalid for the current
// sender bucket. When PrepareProposal hits one of these on an EVM tx, the
// Bug B cascade-skip drops the entire sender bucket instead of advancing
// to the next nonce of the same account (which would itself fail).
//
// These substrings span TWO sources:
//   - go-ethereum core: "nonce too high", "nonce too low", "intrinsic gas",
//     "invalid nonce"
//   - cosmos-evm v0.6.0 fork: "nonce is higher than account nonce",
//     "nonce is lower than account nonce" (longer, hand-formatted)
//   - cosmos-sdk core: "invalid sequence"
//
// SA-AUDIT-2026-06-10 fix16 A14-APP-H2 / A14-CROSS-H2: extracted from
// inline app/abci_proposal.go strings into a package-level slice so the
// list is unit-testable. permanent_nonce_err_test.go asserts every
// production-emitted error wording still matches; if an upstream merge
// changes a string and the test fails at PR time, the cascade-skip is
// caught BEFORE it silently degrades in production (fix11 had to add
// fork wording reactively; this test prevents the next reactive cycle).
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
