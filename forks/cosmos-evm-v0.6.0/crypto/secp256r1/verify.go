// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package secp256r1

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"
)

// secp256r1 group order N (NIST P-256).
//
// SA-AUDIT-2026-06-10 fix18 A15-CRYPTO-M1 introduced range checks AND a
// low-S (s <= N/2) requirement.
//
// SA-AUDIT-2026-06-11 fix19 NHÓM-5 Q5=A (user policy decision, memory
// #176): the low-S clause is REMOVED; range checks r,s ∈ (0,n) stay.
// EIP-7212/RIP-7212 §Verification does not mandate low-S for P256VERIFY,
// and the production deployments we benchmark against (Sei, Polygon,
// Optimism, Base, Linea) all accept high-S — WebAuthn authenticators
// routinely emit high-S signatures, so enforcing low-S breaks real
// passkey flows for no consensus-level gain (P256VERIFY is a pure boolean
// oracle; replay protection lives in account nonces, not in signature
// canonicality). Contracts needing a canonical (msg, sig) nullifier must
// normalize S themselves — the same contract-level burden as on every
// other chain. Reversible by re-adding `s.Cmp(N/2) > 0` to the check
// below if policy changes.
var secp256r1N = elliptic.P256().Params().N

// Verifies the given signature (r, s) for the given hash and public key (x, y).
func Verify(hash []byte, r, s, x, y *big.Int) bool {
	// Create the public key format
	publicKey := newECDSAPublicKey(x, y)

	// Check if they are invalid public key coordinates
	if publicKey == nil {
		return false
	}

	// Range check r,s ∈ (0, n) per RIP-7212. High-S accepted (Q5=A above).
	if r == nil || s == nil ||
		r.Sign() <= 0 || s.Sign() <= 0 ||
		r.Cmp(secp256r1N) >= 0 || s.Cmp(secp256r1N) >= 0 {
		return false
	}

	// Verify the signature with the public key,
	// then return true if it's valid, false otherwise
	return ecdsa.Verify(publicKey, hash, r, s)
}

// newECDSAPublicKey creates an ECDSA P256 public key from the given coordinates
func newECDSAPublicKey(x, y *big.Int) *ecdsa.PublicKey {
	// Check if the given coordinates are valid and in the reference point (infinity)
	if x == nil || y == nil || x.Sign() == 0 && y.Sign() == 0 || !elliptic.P256().IsOnCurve(x, y) {
		return nil
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}
}
