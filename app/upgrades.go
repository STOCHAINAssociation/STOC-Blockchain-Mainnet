package app

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	icacontrollertypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/controller/types"
	icahosttypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/host/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"

	evmutiltypes "stoc/x/evmutil/types"
)

// Upgrade history (chronological). Keeping every handler in source is NECESSARY
// but NOT SUFFICIENT for a new node to reach the tip:
//   - NECESSARY: a node that has ALREADY applied an upgrade must still carry that
//     handler or x/upgrade ApplyUpgrade panics "wrong app version, upgrade handler
//     is missing" (cosmossdk.io/x/upgrade abci.go).
//   - NOT SUFFICIENT: a SINGLE binary carrying ALL handlers CANNOT cold-sync from
//     genesis. During every window [gov-prop-exec-height, upgrade-trigger-height)
//     the x/upgrade PreBlocker sees the plan scheduled-but-not-triggered while the
//     handler is present and panics "BINARY UPDATED BEFORE TRIGGER" (BUG-4).
//     Cold-sync of post-upgrade history therefore requires the phased-binary
//     lineage (stoc_p0_genesis_d → stoc_v2_only_d → stoc_fixed_evm_d → … → v5,
//     each carrying handlers only up to its own phase) or cosmovisor/state-sync.
//     (Upgrading the binary before the trigger height panics with "BINARY UPDATED BEFORE TRIGGER".)
//
//   v2-evm                   — mainnet block 4455467 (Mar 2026): introduce EVM
//                              (cosmos/evm rc2 → v0.6.0). Sets MinGasPrice=0.
//   v3-fix-evm-denom         — mainnet block 4705316 (Apr 2026): fix EVM denom
//                              ("atest" → derived from bond_denom). Sets
//                              MinGasPrice=10^9 (assumed 18-decimal scale, only
//                              correct when paired with module-cache sed patch
//                              that bypasses cosmos/evm v0.6.0 wrapper ×10^12).
//   v5.0.0                 — TBD: consolidated post-v3 fixes for mainnet
//                              rollout. Bundles the previously-iterated devnet
//                              work (v4 feemarket source fix, v5 EIP-1559
//                              enable, v6 dust round-up, v7 MaxPendingTxPerWallet
//                              cap + bundle, v8 cosmos/evm fork mempool fixes,
//                              v8.3 Bug B PrepareProposal cascade-skip wire)
//                              into a single gov proposal + binary swap. See
//                              UpgradeNameV5 block below for the full list
//                              of source/param changes shipped.
const (
	UpgradeName            = "v2-evm"
	UpgradeNameFixEVMDenom = "v3-fix-evm-denom"
	// UpgradeNameV5 consolidates all post-v3 fixes into a single mainnet
	// rollout. Replaces the devnet-only v4/v5/v6/v7/v8 iteration chain. Single
	// gov proposal + binary swap delivers:
	//
	// =========================================================================
	// ISSUES FIXED BY v5.0.0 (explicit list):
	// =========================================================================
	//
	//   Issue #1 — feemarket disabled / MetaMask 2-popup UX bug
	//     Symptom: MetaMask opens signing popup twice; first submit fails with
	//              "Gas estimation failed: insufficient funds for intrinsic
	//              transaction cost"; user must click Send Transaction twice.
	//     Root cause: feemarket NoBaseFee=true under v3 era → eth_feeHistory
	//                 returns mixed 0/non-zero baseFeePerGas → MetaMask EIP-1559
	//                 estimator underestimates fee → first submit gets rejected
	//                 by validator min-gas-price → MM auto-retries via gasPrice
	//                 fallback → user sees second popup.
	//     Fix: enable EIP-1559 base_fee with valid params (see params block below).
	//
	//   Issue #2 — wei→udstoc dust precision mismatch
	//     Symptom: EVM tx with non-multiple-of-10^12-wei amount rejected with
	//              "amount X adstoc has dust remainder Y wei"; MetaMask "deduct
	//              full gas cost" error on contract deploy.
	//     Root cause: Cosmos minimum unit = 1 udstoc (10^12 wei) but EVM
	//                 gasPrice operates at 1 gwei (10^9 wei) granularity.
	//                 gasUsed × gasPrice rarely a multiple of 10^12.
	//     Fix: x/evmutil/keeper/bank_keeper.go convertAndValidateCoins rounds
	//          UP wei → udstoc (sender overpays by ≤ 1 udstoc, negligible).
	//
	//   Issue #3 — High-volume sender mempool flood
	//     Symptom: a single wallet submitting many pending EVM transactions can
	//              fill the mempool head and starve other users' transactions
	//              from inclusion.
	//     Root cause: no per-wallet cap on pending EVM mempool txs.
	//     Fix: app/ante/max_pending_tx.go MaxPendingTxPerWalletDecorator cap 50
	//          (configurable). Plus nil-check on pool.GetTxPool() to prevent
	//          startup-race panic during CheckTx.
	//
	//   Issue #4 — Bug A cosmos/evm mempool sum-queued-cost balance check
	//     Symptom: tx rejected even though sender has enough balance for
	//              individual tx, because sum of all pending tx costs exceeds.
	//     Root cause: upstream geth-style aggregate balance check unsuited to
	//                 Cosmos block-by-block balance semantics.
	//     Fix: forks/cosmos-evm-v0.6.0/mempool/txpool/validation.go switches to
	//          per-tx balance check (skip cumulative sum-queued cost).
	//
	//   Issue #5 — Bug B PrepareProposal cascade-skip (PARTIAL → wired)
	//     Symptom: after EVM ante fails for sender A's tx, block builder
	//              iterates ALL of A's subsequent pending txs (also fail with
	//              nonce-too-high cascade), wasting BlockBuilding time.
	//     Root cause: upstream cosmos/evm v0.6.0 only exposed PopCurrentAccount
	//                 helper on EVMMempoolIterator — no caller invokes it.
	//     Fix: app/abci_proposal.go STOCProposalHandler wraps DefaultProposal
	//          handler, calls iter.PopCurrentAccount() when EVM ante fails.
	//          Cascade-skip activates at runtime.
	//
	//   Issue #6 — Bug C eth_getTransactionCount pending nonce mismatch
	//     Symptom: MetaMask gets stale nonce from chain (does not include
	//              pending EVM mempool entries), submits tx with wrong nonce
	//              → rejected → user confused.
	//     Root cause: upstream cosmos/evm v0.6.0 eth_getTransactionCount
	//                 (pending) only consulted CometBFT mempool, missed EVM
	//                 legacypool queued/pending buckets.
	//     Fix: forks/cosmos-evm-v0.6.0/rpc/backend/account_info.go pending
	//          nonce = max(chainNonce, txpool.PoolNonce(addr)).
	//
	//   Issue #7 — L2 legacypool stuck pending eviction
	//     Symptom: a wallet whose balance is depleted by other operations
	//              accumulates pending transactions (cumulative cost > balance)
	//              that never evict from the legacy pool, allowing the pool to
	//              grow unbounded over time.
	//     Root cause: upstream go-ethereum legacypool eviction loop only
	//                 scanned pool.queue bucket, not pool.pending. Assumption
	//                 (pending = always-mineable) breaks for Cosmos-EVM where
	//                 a permanently-unminable pending tx is possible.
	//     Fix: forks/cosmos-evm-v0.6.0/mempool/txpool/legacypool/legacypool.go
	//          extend evict loop to also scan pool.pending and drop entries
	//          where beats[addr] idle > Lifetime. Lifetime = 1h (matches
	//          CometBFT ttl-duration for consistency).
	//
	//   Issue #8 — L1 CometBFT mempool orphan eviction
	//     Symptom: a transaction may live in the CometBFT mempool while the
	//              legacy pool has already evicted it (cumulative-balance reject,
	//              replacement, pool lifetime). Single-tx ante validation passes
	//              on Recheck so the CometBFT mempool retains the entry; the
	//              proposer skips it every block because L2's view disagrees.
	//              Without explicit cross-layer signalling, the CometBFT mempool
	//              grows unbounded.
	//     Root cause: no propagation from legacypool removal → CometBFT
	//                 mempool. CheckTx wrapper had no orphan detection.
	//     Fix: forks/cosmos-evm-v0.6.0/mempool/check_tx.go on RecheckTx flag,
	//          query legacypool.Has(hash) — if false, return ErrInvalidRequest
	//          so CometBFT drops from its mempool. Companion helper
	//          ExperimentalEVMMempool.IsOrphanEVMTx() exposes lookup.
	//
	// =========================================================================
	// State changes applied by v5.0.0 handler:
	// =========================================================================
	//
	//   Gov-visible params:
	//     - feemarket.NoBaseFee   = false   (enable EIP-1559)
	//     - feemarket.BaseFee     = 0.001   (Cosmos scale: 0.001 ustoc/gas = 1 gwei effective)
	//     - feemarket.MinGasPrice = 0.001   (same — feemarket floor for mempool admission)
	//
	//     Verified live devnet 2026-05-29: FeeChecker (in NewDeductFeeDecorator)
	//     compares feeCap vs BaseFee in Cosmos scale directly; 0.001 enforces 1 gwei.
	//     Custom CosmosMinGasPriceDecorator's .Quo(10^12) is dead code (silent passes;
	//     FeeChecker catches with correct value afterward). SA-C1 audit finding was
	//     a false positive — the dead decorator misled the static analysis.
	//
	//   Source-only changes that ride along with the binary (no params needed):
	//     - x/evmutil/keeper/bank_keeper.go     — Issue #2 wei→udstoc round-up
	//     - app/ante/max_pending_tx.go          — Issue #3 MaxPendingTxPerWallet
	//                                              + nil-check defense
	//     - app/abci_proposal.go                — Issue #5 Bug B cascade-skip
	//                                              wire (STOCProposalHandler)
	//     - forks/cosmos-evm-v0.6.0/...         — Issues #4 + #5-helper + #6
	//
	// Idempotent: re-running the handler is safe — params are deterministic
	// re-assertions and the source-only changes activate purely on binary load.
	//
	// MAINNET ROLLOUT: 1 gov prop applying this handler on top of current
	// mainnet v3-fix-evm-denom state → 1 halt + 1 binary swap → all 6 issues
	// fixed at once.
	UpgradeNameV5 = "v5.0.0"

	// UpgradeNameV503 ships the cosmos/evm v0.6.4 security fixes. They change
	// state-transition results, so every validator must switch binary at the
	// same height through x/upgrade:
	//   - GHSA-p8x8-4gg6-prcc: internal EVM calls (CallEVMWithData) are bounded
	//     by the tx gas meter; commit/flush errors charge the consumed gas.
	//   - GHSA-7xxp-gq2c-x47x: EIP-712 sign docs reject timeout, unordered,
	//     fee granter and fee payer fields that the typed data does not cover.
	//   - GHSA-c6xv-pppp-rjw6: self-destruct is refused for contracts that hold
	//     delegations, redelegations or unbonding delegations.
	//   - GHSA-wm4f-qpxp-rch4: the per-tx precompile call counter cannot wrap
	//     around and reopen the call limit.
	//
	// It also ships the STOChain audit round 1 fixes (2026-11-05). Source-only:
	// transfer/ICA module accounts blocked, custom tokens rejected for module
	// addresses, group policies and EVM contracts, tax leg fail-closed, tax
	// recipient validated at MsgCreateToken, custom-token IBC escrow check
	// through a persistent escrow index in the x/stoc store (one KV lookup,
	// written when a transfer channel is created), TraceBlock height bounded,
	// non-empty ActiveStaticPrecompiles rejected by EVM params validation, IBC
	// v2 messages rejected in the ante. State changes made by the handler (no
	// store added, no module version bumped):
	//   - every module account in the permission list exists as a module
	//     account; a BaseAccount squatting a module address (no pubkey) is
	//     converted in place, keeping number, sequence and balance;
	//   - ICA host disabled with an empty allowlist, ICA controller disabled;
	//   - IBC client AllowedClients = ["07-tendermint"];
	//   - the missing tax recipient account of every taxed token is created
	//     as a plain BaseAccount (so it cannot later be turned into a locked
	//     vesting account);
	//   - EVM ActiveStaticPrecompiles reset to [] when not already empty
	//     (already [] on mainnet and testnet);
	//   - the escrow address of every existing transfer-port channel (any
	//     state) is written to the x/stoc escrow index (mainnet has none);
	//   - x/erc20 PermissionlessRegistration = false (only governance can
	//     register an ERC-20 token pair); EnableErc20 and the other erc20
	//     state are unchanged (mainnet: enable_erc20=true,
	//     permissionless_registration=true, 0 token pairs).
	UpgradeNameV503 = "v5.0.3"

	// UpgradeNameV504 is a one-time foundation burn: the handler burns a fixed
	// amount of ustoc from 7 hardcoded foundation wallets (see
	// v504FoundationBurns). It changes no module, parameter or store layout.
	UpgradeNameV504 = "v5.0.4"
)

// RegisterUpgradeHandlers registers the upgrade handlers for the app.
func (app *App) RegisterUpgradeHandlers() {
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeName,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// Run module migrations first (initializes EVM, feemarket, erc20, evmutil stores)
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			// SA-C3 audit-2026-05-29: wrap discretionary post-migration writes in
			// CacheContext so SetParams + setEvmDenomFromStaking commit atomically.
			// RunMigrations stays outside (idempotent per SDK contract — re-runs
			// against new fromVM skip already-migrated modules).
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			cacheCtx, writeCache := sdkCtx.CacheContext()

			// Set feemarket params: disable dynamic base fee for initial EVM deployment.
			// This ensures existing gas price config (0.01 ustoc) in wallets/FE remains valid.
			// Can be re-enabled later via governance proposal once ecosystem is ready.
			//
			// Read existing params and apply targeted overrides so any future governance
			// changes to unrelated fields are preserved across handler re-runs (audit fix H3).
			feemarketParams := app.FeeMarketKeeper.GetParams(cacheCtx)
			// SA-AUDIT-2026-06-05 M2: mirror v5.0.0 defensive checks (lines
			// 279-284) — abort if upstream migration left a divide-by-zero
			// invariant rather than masking it with our overwrites.
			if feemarketParams.BaseFeeChangeDenominator == 0 {
				return vm, fmt.Errorf("v2-evm upgrade aborted: post-migration feemarket BaseFeeChangeDenominator=0 (divide-by-zero); restore from snapshot")
			}
			if feemarketParams.ElasticityMultiplier == 0 {
				return vm, fmt.Errorf("v2-evm upgrade aborted: post-migration feemarket ElasticityMultiplier=0 (freezes base-fee); restore from snapshot")
			}
			feemarketParams.NoBaseFee = true
			feemarketParams.BaseFee = math.LegacyZeroDec()
			feemarketParams.MinGasPrice = math.LegacyZeroDec()
			// SA-AUDIT-2026-06-10 fix16 A14-APP-M5: full Validate() catches
			// any other internal invariant the cherry-picked checks above
			// don't cover (MinBaseFee bounds, gas-target relationships).
			if err := feemarketParams.Validate(); err != nil {
				return vm, fmt.Errorf("v2-evm upgrade aborted: post-migration feemarket params invalid: %w", err)
			}
			if err := app.FeeMarketKeeper.SetParams(cacheCtx, feemarketParams); err != nil {
				return vm, fmt.Errorf("failed to set feemarket params: %w", err)
			}

			// Fix EVM denom: sdk.DefaultBondDenom may not be set during upgrade init,
			// causing default "atest" instead of correct denom.
			// Derive from staking bond_denom (already loaded from genesis).
			if err := setEvmDenomFromStaking(app, cacheCtx); err != nil {
				return vm, err
			}

			writeCache()
			return vm, nil
		},
	)

	// v3-fix-evm-denom: fix EVM denom and MinGasPrice
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameFixEVMDenom,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			// SA-C3 audit-2026-05-29: wrap discretionary post-migration writes
			// (setEvmDenomFromStaking + SetParams) in CacheContext.
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			cacheCtx, writeCache := sdkCtx.CacheContext()

			if err := setEvmDenomFromStaking(app, cacheCtx); err != nil {
				return vm, err
			}

			// Fix feemarket MinGasPrice: v2-evm set it to 0 allowing free EVM tx spam.
			// Set 10^9 astoc/gas = 0.001 ustoc/gas = 1 gwei, matching Cosmos min-gas-prices.
			//
			// Read existing params and apply targeted overrides so any future governance
			// changes to unrelated fields are preserved across handler re-runs (audit fix H3).
			feemarketParams := app.FeeMarketKeeper.GetParams(cacheCtx)
			// SA-AUDIT-2026-06-05 M3: mirror v5.0.0 defensive checks — abort
			// if upstream migration left a divide-by-zero invariant.
			if feemarketParams.BaseFeeChangeDenominator == 0 {
				return vm, fmt.Errorf("v3-fix-evm-denom upgrade aborted: post-migration feemarket BaseFeeChangeDenominator=0 (divide-by-zero); restore from snapshot")
			}
			if feemarketParams.ElasticityMultiplier == 0 {
				return vm, fmt.Errorf("v3-fix-evm-denom upgrade aborted: post-migration feemarket ElasticityMultiplier=0 (freezes base-fee); restore from snapshot")
			}
			feemarketParams.NoBaseFee = true
			feemarketParams.BaseFee = math.LegacyZeroDec()
			feemarketParams.MinGasPrice = math.LegacyNewDec(1_000_000_000)
			// SA-AUDIT-2026-06-10 fix16 A14-APP-M5: full Validate() defense-in-depth.
			if err := feemarketParams.Validate(); err != nil {
				return vm, fmt.Errorf("v3-fix-evm-denom upgrade aborted: post-migration feemarket params invalid: %w", err)
			}
			if err := app.FeeMarketKeeper.SetParams(cacheCtx, feemarketParams); err != nil {
				return vm, fmt.Errorf("failed to set feemarket MinGasPrice: %w", err)
			}

			writeCache()
			return vm, nil
		},
	)

	// v5.0.0: consolidated post-v3 rollout. See UpgradeNameV5 const block
	// above for the full list of source/param changes. Handler only writes the
	// feemarket params here — source-only changes (dust round-up,
	// MaxPendingTxPerWallet, Bug B wire, fork mempool fixes) activate at binary
	// load and need no migration call.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameV5,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			// SA-C3 audit-2026-05-29: wrap discretionary post-migration writes
			// in CacheContext. Only 1 write here but consistent pattern across handlers.
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			cacheCtx, writeCache := sdkCtx.CacheContext()

			// SA-2026-06-02 MED-5 (senior-skeptic audit): defense-in-depth
			// re-read of feemarket params before we SetParams. If an
			// upstream migration inside RunMigrations bumps the schema and
			// happens to reset a field we care about (BaseFeeChangeDenominator,
			// ElasticityMultiplier) to a known-broken zero value, our
			// overwrites below would mask the breakage and ship a chain
			// with a silently-zeroed knob. Read first, validate the
			// non-overwritten fields, then patch only the ones this handler
			// owns. If the read itself fails or returns a struct that fails
			// internal validation, halt the upgrade loudly so operators
			// recover from snapshot instead of booting a half-upgraded chain.
			params := app.FeeMarketKeeper.GetParams(cacheCtx)
			if params.BaseFeeChangeDenominator == 0 {
				return vm, fmt.Errorf("v5.0.0 upgrade aborted: post-migration feemarket params have BaseFeeChangeDenominator=0 (would divide-by-zero at next base-fee update); upstream migration is corrupt — restore from snapshot")
			}
			if params.ElasticityMultiplier == 0 {
				return vm, fmt.Errorf("v5.0.0 upgrade aborted: post-migration feemarket params have ElasticityMultiplier=0 (would freeze base-fee adjustment); upstream migration is corrupt — restore from snapshot")
			}

			// SA-C1 REVERT (audit 2026-05-29 + live devnet test):
			// The fee-enforcement authority is `evmante.FeeChecker` (wired via
			// `NewDeductFeeDecorator`'s txFeeChecker) — see
			// forks/cosmos-evm-v0.6.0/ante/evm/fee_checker.go:99 `feeCap.LT(baseFee)`.
			// FeeChecker uses BaseFee in COSMOS SCALE (ustoc/gas) DIRECTLY.
			// Therefore 0.001 raw Dec = 0.001 ustoc/gas = 1 gwei effective floor. CORRECT.
			// The STOChain custom `CosmosMinGasPriceDecorator.Quo(10^12)` is a no-op
			// in practice — it runs before FeeChecker but its `.Quo` silent-passes;
			// FeeChecker catches afterward at the right value.
			// Live devnet 2026-05-29: tx with 1 udstoc fee + 200k gas rejected
			// "got: 0.000005 udstoc/gas required: 0.001 udstoc/gas" — proves enforcement works.
			// Reverting to 0.001 Dec.
			params.NoBaseFee = false
			params.BaseFee = math.LegacyNewDecWithPrec(1, 3)     // 0.001 ustoc/gas = 1 gwei effective
			params.MinGasPrice = math.LegacyNewDecWithPrec(1, 3) // same — feemarket floor
			// SA-AUDIT-2026-06-10 fix16 A14-APP-M5: full Validate() defense-in-depth.
			if err := params.Validate(); err != nil {
				return vm, fmt.Errorf("v5.0.0 upgrade aborted: post-migration feemarket params invalid: %w", err)
			}
			if err := app.FeeMarketKeeper.SetParams(cacheCtx, params); err != nil {
				return vm, fmt.Errorf("failed to apply v5.0.0 feemarket params: %w", err)
			}

			writeCache()
			return vm, nil
		},
	)

	// v5.0.3: security fixes (see UpgradeNameV503). Source-only rules activate
	// when the upgraded binary resumes at the upgrade height; the handler
	// applies the matching state changes.
	app.UpgradeKeeper.SetUpgradeHandler(UpgradeNameV503, app.upgradeHandlerV503)

	// v5.0.4: one-time foundation burn (see UpgradeNameV504).
	app.UpgradeKeeper.SetUpgradeHandler(UpgradeNameV504, app.upgradeHandlerV504)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("failed to read upgrade info from disk: %s", err))
	}

	if upgradeInfo.Name == UpgradeName && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{
				evmtypes.ModuleName,
				feemarkettypes.ModuleName,
				erc20types.ModuleName,
				evmutiltypes.ModuleName,
			},
		}

		// Configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	// v3-fix-evm-denom + v5.0.0 + v5.0.3 + v5.0.4: no new stores needed.
}

// upgradeHandlerV503 is the v5.0.3 upgrade handler. Idempotent: running it
// again on the resulting state changes nothing.
func (app *App) upgradeHandlerV503(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Module accounts first: a squatted module address makes
	// GetModuleAccount panic, and migrations may call it.
	cacheCtx, writeCache := sdkCtx.CacheContext()
	if err := app.ensureModuleAccounts(cacheCtx); err != nil {
		return fromVM, fmt.Errorf("v5.0.3 upgrade aborted: %w", err)
	}
	writeCache()

	vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
	if err != nil {
		return vm, err
	}

	cacheCtx, writeCache = sdkCtx.CacheContext()
	if err := app.applyV503SecurityState(cacheCtx); err != nil {
		return vm, fmt.Errorf("v5.0.3 upgrade aborted: %w", err)
	}
	writeCache()
	return vm, nil
}

// ensureModuleAccounts makes sure every module account of the permission
// list exists as a module account (v5.0.3 audit round 1, module-account
// squatting). Lazily created module accounts (ibc transfer,
// interchainaccounts, ...) could be pre-empted by a plain send, which left a
// BaseAccount at the module address; GetModuleAccount then panics.
//
//   - missing: the module account is created (GetModuleAccount);
//   - already a module account: left as is;
//   - any other account type without a public key: replaced in place by the
//     module account, keeping account number, sequence and every balance
//     (balances live in x/bank under the address and are not touched).
//     Nobody can hold a private key for a module address, so a pubkey there
//     is impossible; if one is found the upgrade aborts instead of guessing.
//
// The permission list is a slice, so iteration order is deterministic.
func (app *App) ensureModuleAccounts(ctx sdk.Context) error {
	for _, p := range moduleAccPerms {
		name := p.Account
		addr := authtypes.NewModuleAddress(name)
		acc := app.AccountKeeper.GetAccount(ctx, addr)

		if acc == nil {
			if app.AccountKeeper.GetModuleAccount(ctx, name) == nil {
				return fmt.Errorf("module account %q could not be created (not in the permission list)", name)
			}
			ctx.Logger().Info("v5.0.3: created missing module account", "module", name, "address", addr.String())
			ctx.EventManager().EmitEvent(sdk.NewEvent("upgrade_v503_module_account_created",
				sdk.NewAttribute("module", name),
				sdk.NewAttribute("address", addr.String()),
			))
			continue
		}

		if _, isModule := acc.(sdk.ModuleAccountI); isModule {
			continue
		}

		if acc.GetPubKey() != nil {
			return fmt.Errorf("module address %s (%s) holds a %T with a public key; refusing to convert", addr.String(), name, acc)
		}

		macc := authtypes.NewModuleAccount(
			authtypes.NewBaseAccount(addr, nil, acc.GetAccountNumber(), acc.GetSequence()),
			name, p.Permissions...,
		)
		if err := macc.Validate(); err != nil {
			return fmt.Errorf("module account %q: %w", name, err)
		}
		app.AccountKeeper.SetAccount(ctx, macc)

		ctx.Logger().Warn("v5.0.3: converted squatted module address back to its module account",
			"module", name, "address", addr.String(), "previous_type", fmt.Sprintf("%T", acc),
			"account_number", acc.GetAccountNumber(), "sequence", acc.GetSequence())
		ctx.EventManager().EmitEvent(sdk.NewEvent("upgrade_v503_module_account_restored",
			sdk.NewAttribute("module", name),
			sdk.NewAttribute("address", addr.String()),
			sdk.NewAttribute("previous_type", fmt.Sprintf("%T", acc)),
		))
	}
	return nil
}

// applyV503SecurityState writes the v5.0.3 parameter and account changes
// that are not module accounts (see UpgradeNameV503).
func (app *App) applyV503SecurityState(ctx sdk.Context) error {
	// ICA host off: the host executed foreign-chain messages without the ante
	// chain or the tax PostHandler (live on mainnet with allow_messages ["*"]).
	hostParams := icahosttypes.NewParams(false, []string{})
	if err := hostParams.Validate(); err != nil {
		return fmt.Errorf("ica host params: %w", err)
	}
	app.ICAHostKeeper.SetParams(ctx, hostParams)
	app.ICAControllerKeeper.SetParams(ctx, icacontrollertypes.NewParams(false))

	// IBC light clients: 07-tendermint only (no solo machine self-signed proofs).
	clientParams := ibcclienttypes.NewParams(stocIBCAllowedClients...)
	if err := clientParams.Validate(); err != nil {
		return fmt.Errorf("ibc client params: %w", err)
	}
	app.IBCKeeper.ClientKeeper.SetParams(ctx, clientParams)

	// Escrow index for channels opened before v5.0.3 (new ones are indexed
	// by the transfer channel guard). Before the tax recipient pre-flight so
	// an escrow used as tax recipient is reported.
	if n := app.indexTransferEscrows(ctx); n > 0 {
		ctx.Logger().Info("v5.0.3: indexed existing transfer channel escrows", "channels", n)
	}

	if err := app.materializeTaxRecipients(ctx); err != nil {
		return err
	}

	// ERC-20 token pair registration becomes governance-only. Only written
	// when it differs, so the handler stays idempotent.
	erc20Params := app.Erc20Keeper.GetParams(ctx)
	if erc20Params.PermissionlessRegistration {
		erc20Params.PermissionlessRegistration = false
		if err := app.Erc20Keeper.SetParams(ctx, erc20Params); err != nil {
			return fmt.Errorf("disable erc20 permissionless registration: %w", err)
		}
		ctx.Logger().Info("v5.0.3: erc20 permissionless registration disabled",
			"enable_erc20", erc20Params.EnableErc20)
	}

	// Static precompiles are never active (already [] on mainnet/testnet).
	// Only write when needed so mainnet/testnet EVM params are untouched.
	evmParams := app.EVMKeeper.GetParams(ctx)
	if len(evmParams.ActiveStaticPrecompiles) > 0 {
		evmParams.ActiveStaticPrecompiles = []string{}
		if err := app.EVMKeeper.SetParams(ctx, evmParams); err != nil {
			return fmt.Errorf("reset evm active static precompiles: %w", err)
		}
		ctx.Logger().Warn("v5.0.3: deactivated all EVM static precompiles")
	}
	return nil
}

// materializeTaxRecipients creates the account of every taxed token's tax
// recipient that does not exist yet, as a plain BaseAccount. A missing tax
// recipient could otherwise be turned into a vesting / permanently locked
// account by anyone (those msgs only work on non-existent addresses) and,
// with the fail-closed tax leg, freeze every transfer of the token.
// Recipients that exist but are not plain wallets are logged and left as is.
// Tokens are iterated in store order (deterministic).
func (app *App) materializeTaxRecipients(ctx sdk.Context) error {
	moduleAddrs := moduleAccountAddrs()
	for _, token := range app.StocKeeper.GetAllTokens(ctx) {
		if token.Tax.Percent.IsNil() || !token.Tax.Percent.IsPositive() || token.Tax.RecipientAddress == "" {
			continue
		}
		addr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if err != nil {
			reportTaxRecipientNotWallet(ctx, token.MinimalDenom, token.Tax.RecipientAddress, "invalid address: "+err.Error())
			continue
		}
		if _, isModule := moduleAddrs[string(addr)]; isModule ||
			app.BankKeeper.BlockedAddr(addr) || app.blockedPrecompileAddrs[addr.String()] ||
			app.StocKeeper.IsIBCEscrow(ctx, addr) {
			reportTaxRecipientNotWallet(ctx, token.MinimalDenom, addr.String(), "module, blocked, precompile or IBC escrow address")
			continue
		}

		acc := app.AccountKeeper.GetAccount(ctx, addr)
		if acc == nil {
			app.AccountKeeper.SetAccount(ctx, app.AccountKeeper.NewAccountWithAddress(ctx, addr))
			ctx.Logger().Info("v5.0.3: created tax recipient account", "denom", token.MinimalDenom, "recipient", addr.String())
			ctx.EventManager().EmitEvent(sdk.NewEvent("upgrade_v503_tax_recipient_created",
				sdk.NewAttribute("denom", token.MinimalDenom),
				sdk.NewAttribute("recipient", addr.String()),
			))
			continue
		}
		// Pre-flight (audit round 2): the same checks MsgCreateToken now
		// applies. A recipient that is not a plain wallet (group policy,
		// module credential, EVM contract, vesting...) cannot receive the
		// tax, so with the fail-closed tax leg every taxed transfer of the
		// token fails. Reported, never aborts the upgrade (mainnet ASL_8's
		// recipient is a BaseAccount with a secp256k1 key and no code,
		// measured 2026-11-05; mainnet has no groups).
		if _, isBase := acc.(*authtypes.BaseAccount); !isBase {
			reportTaxRecipientNotWallet(ctx, token.MinimalDenom, addr.String(), fmt.Sprintf("not a plain wallet (%T)", acc))
			continue
		}
		if err := app.customTokenWallets.checkTaxRecipient(ctx, addr); err != nil {
			reportTaxRecipientNotWallet(ctx, token.MinimalDenom, addr.String(), err.Error())
		}
	}
	return nil
}

// reportTaxRecipientNotWallet logs at Error level and emits
// upgrade_v503_tax_recipient_not_wallet for a taxed token whose tax recipient
// cannot receive custom tokens. Taxed transfers of that token will fail
// (fail-closed tax leg); operators must act on this event before mainnet.
func reportTaxRecipientNotWallet(ctx sdk.Context, denom, recipient, reason string) {
	ctx.Logger().Error("v5.0.3: token tax recipient is not a regular user wallet; taxed transfers of this token will fail",
		"denom", denom, "recipient", recipient, "reason", reason)
	ctx.EventManager().EmitEvent(sdk.NewEvent("upgrade_v503_tax_recipient_not_wallet",
		sdk.NewAttribute("denom", denom),
		sdk.NewAttribute("recipient", recipient),
		sdk.NewAttribute("reason", reason),
	))
}

// setEvmDenomFromStaking derives and sets EVM denom config from staking bond_denom.
// For 6-decimal chains: evm_denom=ustoc (Cosmos), extended_denom=astoc (18-dec EVM).
// Also sets bank denom_metadata and initializes EVM coin info in KV store.
func setEvmDenomFromStaking(app *App, sdkCtx sdk.Context) error {
	if app.StakingKeeper == nil {
		return fmt.Errorf("staking keeper not initialized during upgrade")
	}
	if app.EVMKeeper == nil {
		return fmt.Errorf("evm keeper not initialized during upgrade")
	}

	stakingParams, err := app.StakingKeeper.GetParams(sdkCtx)
	if err != nil {
		return fmt.Errorf("failed to get staking params: %w", err)
	}

	bondDenom := stakingParams.BondDenom
	if len(bondDenom) < 2 || bondDenom[0] != 'u' {
		return fmt.Errorf("invalid bond_denom %q: must start with 'u' (e.g. 'ustoc', 'utstoc')", bondDenom)
	}
	extendedDenom := "a" + bondDenom[1:] // "ustoc" → "astoc", "utstoc" → "atstoc"
	displayDenom := bondDenom[1:]        // "ustoc" → "stoc", "utstoc" → "tstoc"

	// Set EVM params: evm_denom = Cosmos base denom, extended_denom = 18-decimal EVM denom
	evmParams := app.EVMKeeper.GetParams(sdkCtx)
	evmParams.EvmDenom = bondDenom
	evmParams.ExtendedDenomOptions = &evmtypes.ExtendedDenomOptions{
		ExtendedDenom: extendedDenom,
	}
	if err := app.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
		return fmt.Errorf("failed to set evm params: %w", err)
	}

	// Set bank denom_metadata — required for InitEvmCoinInfo to load decimals + display denom.
	//
	// SA-2026-06-02 MED-4 (senior-skeptic audit): the prior version constructed
	// a fresh Metadata literal with zero-valued Description / URI / URIHash and
	// passed it directly to SetDenomMetaData, which blanket-overwrites the bank
	// store entry. Every time this handler ran (v2-evm, v3-fix-evm-denom, and
	// v5.0.0 — and any cold-sync replay through all three) the chain silently
	// dropped governance-curated metadata (chain registry description, logo
	// URI, sha256 URI hash) that explorers + Keplr UI consume. Business rule
	// (locked 2026-06-02): READ the existing metadata first, mutate only the
	// fields this handler actually owns (Base/Display/DenomUnits/Name/Symbol),
	// and write back. Description / URI / URIHash survive untouched. If no
	// metadata existed previously we write a fresh zero-value record, matching
	// prior behaviour for a brand-new chain.
	existing, foundMeta := app.BankKeeper.GetDenomMetaData(sdkCtx, bondDenom)
	existing.Base = bondDenom
	existing.Display = displayDenom
	if !foundMeta {
		// SA-AUDIT-2026-06-10 fix16 A14-CROSS-H3: previously this handler
		// UNCONDITIONALLY overwrote Name/Symbol/DenomUnits on every replay
		// of this upgrade height. Any subsequent governance MsgSetDenomMetadata
		// (e.g. adding an "mstoc" DenomUnit, renaming Symbol) would be
		// silently reverted whenever a new validator cold-synced from genesis
		// through this upgrade height — producing app-hash divergence vs
		// live-synced nodes on the next block that read metadata-derived
		// state. Only write the full block on first-time init; on replay,
		// leave Name/Symbol/DenomUnits alone and let governance own them.
		existing.DenomUnits = []*banktypes.DenomUnit{
			{Denom: bondDenom, Exponent: 0},
			{Denom: displayDenom, Exponent: 6},
		}
		existing.Name = strings.ToUpper(displayDenom)
		existing.Symbol = strings.ToUpper(displayDenom)
	}
	app.BankKeeper.SetDenomMetaData(sdkCtx, existing)

	// Initialize EVM coin info from bank metadata + params → stores in KV store
	if err := app.EVMKeeper.InitEvmCoinInfo(sdkCtx); err != nil {
		return fmt.Errorf("failed to init evm coin info: %w", err)
	}

	return nil
}
