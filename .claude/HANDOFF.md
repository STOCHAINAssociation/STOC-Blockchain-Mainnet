# HANDOFF

> 2026-03-09

## CURRENT REQUEST

Full line-by-line code review of stochain blockchain source code (excluding FE), fix all issues, commit/push with severity notes, loop audit until clean, post review on PR #73. Then block custom token IBC transfers per user request.

## PLAN

- [x] Rounds 1-20: Previous session — 13+ issues fixed
- [x] Round 21: Full audit — 2 MEDIUM, 1 LOW fixed → `c41eed8`
- [x] Round 22: Fresh parallel agent — 1 HIGH, 1 MEDIUM fixed → `093c4c9`
- [x] Round 23: Fresh parallel agent — 1 MEDIUM fixed → `999442e`
- [x] Round 24: Final verification audit — **CLEAN, 0 actionable issues**
- [x] PR #73 comments posted (R21-24 report + @claude mention)
- [x] feat: Block custom token IBC transfers → `cc6c47a`
- [x] Research: Kava/Sei/Osmosis TokenFactory comparison

## COMPLETED WORK

### Total: 19 audit issues fixed + 1 feature (IBC restriction)

| Severity | Count | Key Issues |
|----------|-------|------------|
| CRITICAL | 2 | Precompile blocking (SendRestriction), CEI in CreateToken |
| HIGH | 3 | Authz tax evasion, missing SetProcessProposal, BalancesWithMetadata memory DoS |
| MEDIUM | 9 | IBC tax calc, MaxTaxPercent cap, genesis dupes, testnet fixes, token counter CEI, native denom fast-path, min 1-unit tax (ante+IBC) |
| LOW | 5 | ReleaseTokens validation, BurnToken docs, FindAccount panic, export.go err check, testnet SetValidator err |
| FEATURE | 1 | Block custom token IBC transfers (IBCCustomTokenRestriction ante decorator) |

### Recurring Issues Analysis
| Issue | Times Raised | Resolution |
|-------|-------------|------------|
| IBC + Tax | 6 rounds | RESOLVED — custom tokens now blocked from IBC entirely |
| CEI pattern | 4 rounds | Each was a different function — progressive hardening |
| Counter overflow | 3 rounds | Progressive: detect → fail-fast → saturation |
| Burn/Supply | 3 rounds | Complex logic, each round fixed deeper edge case |

### Key Commits on `feat/evm`
- `bd27ca5` — Round 1: IBC tax calc, runtime cap, dead code, pagination
- `2dcfca9` — Round 3: IBC MaxTaxPercent cap, MsgBurnToken denom validation
- `5dcedd1` — Round 4: Critical/high/medium security fixes
- `c9f8f60` — Round 5: Critical/high/medium security fixes
- `f26967c` — Round 6: CRITICAL precompile blocking + HIGH/MEDIUM fixes
- `6cc2774` — Round 7: Genesis duplicate check, ReleaseTokens validation
- `a6846f3` — Round 8: SetTokenCounter CEI pattern
- Rounds 9-20: MustUnmarshal panics, burn invariant, counter overflow, etc.
- `c41eed8` — Round 21: Native denom fast-path, error handling
- `093c4c9` — Round 22: Memory DoS fix, minimum tax enforcement
- `999442e` — Round 23: IBC minimum tax enforcement
- `cc6c47a` — feat: Block custom token IBC transfers

### PR #73
- Branch: `feat/evm` -> `main`
- Status: OPEN
- 4+ review comments posted
- URL: https://github.com/MinhAnh-Corp/stochain/pull/73

## FILES MODIFIED (all audit rounds + IBC feature + code review fixes)

### Source Files
- `app/app.go` — blockedPrecompileAddrs, StocAnteOptions with StocKeeper
- `app/app_config.go` — EVM module accounts
- `app/evm.go` — SendRestriction, SetProcessProposal, precompile blocklist, GetMempool nil-safe fallback
- `app/ante/ante.go` — StocAnteOptions, reject multiple extension options, doc comment typo fix
- `app/ante/cosmos_handler.go` — IBCCustomTokenRestriction in decorator chain
- `app/ante/evm_handler.go` — Accept StocAnteOptions
- `app/export.go` — Error check after 2nd IterateValidators, iter.Close() error handling
- `x/stoc/ante/ibc_restriction.go` — NEW: block custom token IBC transfers
- `x/stoc/ante/tax_post.go` — Authz handling, native denom fast-path, min 1-unit tax, micro-transfer early exit
- `x/stoc/keeper/msg_server_create_token.go` — CEI pattern
- `x/stoc/keeper/msg_server_burn_token.go` — Supply underflow, excess burn, pre-validation comment clarification
- `x/stoc/keeper/msg_server_release_token.go` — Positive amount check
- `x/stoc/keeper/query_token.go` — IterateAccountBalances early termination
- `x/stoc/types/genesis.go` — Duplicate MinimalDenom check
- `x/stoc/types/token.go` — Creator validation, MaxDistributions, Validate() logo scheme + native denom check
- `x/stoc/types/expected_keepers.go` — IterateAccountBalances interface
- `x/stoc/module/genesis.go` — Counter restoration fallback
- `x/stoc/module/autocli.go` — PositionalArgs for Token and TokensBySymbol CLI commands
- `x/stoc/simulation/helpers.go` — FindAccount no-panic
- `cmd/stocd/cmd/testnet.go` — DelegatorShares, SetValidator error check

### Test Files
- `x/stoc/ante/ibc_restriction_test.go` — Tests for IBC custom token blocking (native allowed, custom blocked, non-transfer passthrough, unknown denom allowed)
- `x/stoc/ante/tax_post_test.go` — Tax post-decorator tests (authz unwrapping, native denom skip, min tax, multi-send)
- `x/stoc/keeper/msg_server_burn_token_test.go` — Burn token tests (supply underflow, excess burn, burn-all)
- `x/stoc/keeper/msg_server_create_token_test.go` — Token creation tests (validation, distribution, CEI pattern)
- `x/stoc/keeper/msg_server_mint_token_test.go` — Mint token tests (supply cap, unlimited minting)
- `x/stoc/keeper/msg_server_release_token_test.go` — Release token tests (positive amount, remaining supply)
- `x/stoc/keeper/token_test.go` — Token CRUD tests (SetToken validation, symbol index, FindToken)
- `x/stoc/keeper/invariants_test.go` — Supply invariant tests
- `x/stoc/types/genesis_test.go` — Genesis validation (duplicate denom check)
- `x/stoc/types/msg_validate_basic_test.go` — Message validation tests
- `x/evmutil/types/keys_test.go` — EVM denom conversion tests (global state restore fix)
- `x/evmutil/keeper/bank_keeper_test.go` — EvmBankKeeper tests (setup global state restore fix)

### Configuration & Build
- `config.yml` — Faucet pointed to admin account, validator balances > bonded amount for gas
- `Dockerfile.openapi` — Pinned Ignite version, non-root user, removed insecure curl pipe
- `go.mod` — Pre-release cosmos/evm dependency documented with upgrade tracking comment

### Documentation Updates
- `documents/chain/readme.md` — JSON-RPC default bind to 127.0.0.1, systemd data directory setup (copy ~/.stoc to stoc user home), firewall split by node type (validator vs indexer) with CentOS/RHEL firewalld examples
- `documents/README.md` — Source code link verified (MinhAnh-Corp/stochain is correct)

## BLOCKERS

None — audit complete, IBC restriction implemented.

## NEXT

1. Wait for @claude review on PR #73
2. Merge PR #73 if review passes
3. Consider Dependabot alerts (5 vulnerabilities on default branch)
4. Optional future features: change admin, allowlist/blocklist (per Kava/Sei comparison)
