package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cosmossdk.io/core/appmodule"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/module"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/spf13/cast"

	evmconfig "github.com/cosmos/evm/config"
	evmmempool "github.com/cosmos/evm/mempool"
	"github.com/cosmos/evm/precompiles/bech32"
	"github.com/cosmos/evm/precompiles/p256"
	precompiletypes "github.com/cosmos/evm/precompiles/types"
	srvflags "github.com/cosmos/evm/server/flags"
	erc20 "github.com/cosmos/evm/x/erc20"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	"github.com/cosmos/evm/x/feemarket"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/cosmos/evm/x/vm"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"

	evmutil "stoc/x/evmutil"
	evmutilkeeper "stoc/x/evmutil/keeper"
	evmutiltypes "stoc/x/evmutil/types"
)

// registerEVMModules register EVM keepers and non dependency inject modules.
func (app *App) registerEVMModules(appOpts servertypes.AppOptions) error {
	// chain config — v0.6.0: EVM chain ID from appOpts, denom config moved to state/genesis
	evmChainID := cast.ToUint64(appOpts.Get(srvflags.EVMChainID))
	if evmChainID == 0 {
		// fallback: parse from cosmos chain-id format "name_EVMID-version"
		var err error
		evmChainID, err = getEVMChainID(appOpts)
		if err != nil {
			return fmt.Errorf("failed to get EVM chain ID: %w", err)
		}
	}

	// set up non depinject support modules store keys
	if err := app.RegisterStores(
		storetypes.NewKVStoreKey(evmutiltypes.StoreKey),
		storetypes.NewKVStoreKey(evmtypes.StoreKey),
		storetypes.NewKVStoreKey(feemarkettypes.StoreKey),
		storetypes.NewKVStoreKey(erc20types.StoreKey),
		storetypes.NewTransientStoreKey(evmtypes.TransientKey),
		storetypes.NewTransientStoreKey(feemarkettypes.TransientKey),
	); err != nil {
		return err
	}

	// set up EVM keeper
	tracer := cast.ToString(appOpts.Get(srvflags.EVMTracer))

	app.FeeMarketKeeper = feemarketkeeper.NewKeeper(
		app.appCodec,
		authtypes.NewModuleAddress(govtypes.ModuleName),
		app.GetKey(feemarkettypes.StoreKey),
		app.UnsafeFindStoreKey(feemarkettypes.TransientKey),
	)

	// Initialize EvmutilKeeper for dual token support (ustoc 6 decimals <-> astoc 18 decimals)
	app.EvmutilKeeper = evmutilkeeper.NewKeeper(
		app.appCodec,
		app.GetKey(evmutiltypes.StoreKey),
		app.BankKeeper,
	)

	// Get EvmBankKeeper that wraps BankKeeper with conversion logic
	evmBankKeeper := app.EvmutilKeeper.GetEvmBankKeeper()

	// NOTE: it's required to set up the EVM keeper before the ERC-20 keeper, because it is used in its instantiation.
	app.EVMKeeper = evmkeeper.NewKeeper(
		app.appCodec,
		app.GetKey(evmtypes.StoreKey),
		app.UnsafeFindStoreKey(evmtypes.TransientKey),
		app.GetStoreKeysMap(),
		authtypes.NewModuleAddress(govtypes.ModuleName),
		app.AccountKeeper,
		evmBankKeeper, // Use EvmBankKeeper instead of regular BankKeeper
		app.StakingKeeper,
		app.FeeMarketKeeper,
		&app.ConsensusParamsKeeper,
		&app.Erc20Keeper,
		evmChainID, // v0.6.0: EVM chain ID is now a constructor parameter
		tracer,
	)

	// Set default EVM coin info as fallback for early RPC requests before InitGenesis.
	// For 6-decimal chains: Denom=ustoc (Cosmos), ExtendedDenom=astoc (18-dec EVM).
	app.EVMKeeper.WithDefaultEvmCoinInfo(evmtypes.EvmCoinInfo{
		Denom:         sdk.DefaultBondDenom,            // "ustoc" / "utstoc" / "udstoc"
		ExtendedDenom: evmutiltypes.GetEvmDenom(),      // "astoc" / "atstoc" / "adstoc"
		DisplayDenom:  strings.TrimPrefix(sdk.DefaultBondDenom, "u"), // "stoc" / "tstoc" / "dstoc"
		Decimals:      evmtypes.SixDecimals.Uint32(),   // 6
	})

	app.Erc20Keeper = erc20keeper.NewKeeper(
		app.GetKey(erc20types.StoreKey),
		app.appCodec,
		authtypes.NewModuleAddress(govtypes.ModuleName),
		app.AccountKeeper,
		evmBankKeeper, // Use EvmBankKeeper instead of regular BankKeeper
		app.EVMKeeper,
		app.StakingKeeper,
		&app.TransferKeeper,
	)

	// register evm modules
	if err := app.RegisterModules(
		evmutil.NewAppModule(app.EvmutilKeeper),
		vm.NewAppModule(app.EVMKeeper, app.AccountKeeper, evmBankKeeper, app.AccountKeeper.AddressCodec()),
		feemarket.NewAppModule(app.FeeMarketKeeper),
		erc20.NewAppModule(app.Erc20Keeper, app.AccountKeeper),
	); err != nil {
		return err
	}

	return nil
}

func (app *App) postRegisterEVMModules() error {
	// SA-H19 audit-2026-05-29: status = NOT A REAL BUG (defense-in-depth only).
	// Existing `blockPrecompileTransfers` SendRestriction correctly rejects
	// MsgSend → precompile addr (verified live: "cannot send to EVM precompile
	// address" code:1). Audit concern was MintCoins-direct bypass, but no
	// current module mints directly to precompile addrs. If future modules
	// add precompile-targeted mints, register them in this restriction or
	// add bank BlockedModuleAccountsOverride at depinject time.

	// v0.6.0: Use DefaultStaticPrecompiles with custom bech32 + p256 precompiles
	const bech32PrecompileBaseGas = 6_000

	// secp256r1 precompile as per EIP-7212
	p256Precompile := &p256.Precompile{}

	bech32Precompile, err := bech32.NewPrecompile(bech32PrecompileBaseGas)
	if err != nil {
		return fmt.Errorf("failed to instantiate bech32 precompile: %w", err)
	}

	// v0.6.0: DefaultStaticPrecompiles takes keeper refs + codec
	defaultPrecompiles := precompiletypes.DefaultStaticPrecompiles(
		*app.StakingKeeper,
		app.DistrKeeper,
		app.EvmutilKeeper.GetEvmBankKeeper(), // Use EvmBankKeeper (PreciseBankKeeper equivalent for STOC)
		&app.Erc20Keeper,
		&app.TransferKeeper,
		app.IBCKeeper.ChannelKeeper,
		*app.GovKeeper,
		app.SlashingKeeper,
		app.appCodec,
	)

	// Merge default precompiles with custom ones (bech32 + p256)
	defaultPrecompiles[bech32Precompile.Address()] = bech32Precompile
	defaultPrecompiles[p256Precompile.Address()] = p256Precompile

	app.EVMKeeper.WithStaticPrecompiles(defaultPrecompiles)

	// Build precompile address blocklist for bank SendRestriction
	blockedPrecompiles := make(map[string]bool, len(defaultPrecompiles))
	for addr := range defaultPrecompiles {
		blockedPrecompiles[sdk.AccAddress(addr.Bytes()).String()] = true
	}
	app.blockedPrecompileAddrs = blockedPrecompiles

	return nil
}

// blockPrecompileTransfers registers a bank SendRestriction that prevents
// Cosmos SDK transfers to EVM precompile addresses. Funds sent to precompile
// addresses would be permanently locked since no private key exists for them.
// NOTE: BlockedModuleAccountsOverride cannot be used for this because it calls
// authtypes.NewModuleAddress(name) which re-hashes the address, producing wrong results.
func (app *App) blockPrecompileTransfers() {
	blocked := app.blockedPrecompileAddrs
	app.BankKeeper.AppendSendRestriction(func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
		if blocked[toAddr.String()] {
			return toAddr, fmt.Errorf("cannot send to EVM precompile address %s", toAddr)
		}
		return toAddr, nil
	})
}

// evmMempoolJournalFlag turns the EVM mempool journal on (app.toml
// [evm.mempool] journal = true). Default off.
const evmMempoolJournalFlag = "evm.mempool.journal"

// latestCommittedContext returns a read-only query context at the latest
// committed height that also works right after a restart.
//
// After a restart BaseApp.Init leaves the check state header at height 0 and
// there is no finalize state until the first block, so CreateQueryContext(0)
// fails its latest-header check until the first commit. This uses the same
// branch of the committed store (CacheMultiStoreWithVersion, nothing is ever
// written) without the header check, and fills the header from the last
// commit: height, app hash and block time. Only the EVM mempool uses it, to
// load its pool state at startup. STOChain fix.
func latestCommittedContext(bApp *baseapp.BaseApp) (sdk.Context, error) {
	height := bApp.LastBlockHeight()
	if height <= 0 {
		return sdk.Context{}, errors.New("no committed block yet")
	}
	// Explicit height: the store branch, the header and the block time all
	// refer to the same committed version even if a block commits meanwhile.
	ctx, err := bApp.CreateQueryContextWithCheckHeader(height, false, false)
	if err != nil {
		return sdk.Context{}, err
	}
	header := ctx.BlockHeader()
	header.Height = height
	header.Time = ctx.BlockTime()
	header.AppHash = nil
	if ci, ok := bApp.CommitMultiStore().(interface {
		GetCommitInfo(int64) (*storetypes.CommitInfo, error)
	}); ok {
		if info, err := ci.GetCommitInfo(height); err == nil && info != nil {
			// state sync and `stocd rollback` store commit info without a
			// timestamp: never install a zero block time
			if !info.Timestamp.IsZero() {
				header.Time = info.Timestamp
			}
			header.AppHash = info.CommitID().Hash
		}
	}
	if len(header.AppHash) == 0 {
		if cid := bApp.LastCommitID(); cid.Version == height {
			header.AppHash = cid.Hash
		}
	}
	if len(header.AppHash) == 0 {
		return sdk.Context{}, fmt.Errorf("no commit info for height %d", height)
	}
	if header.Time.IsZero() {
		// no usable block time (e.g. right after state sync or rollback):
		// let the pool wait for the first new block instead
		return sdk.Context{}, fmt.Errorf("no block time for height %d", height)
	}
	return ctx.WithBlockHeader(header).WithBlockHeight(height), nil
}

// evmMempoolJournalPath returns the journal path (empty when disabled or the
// home is unknown) and whether the flag asked for it.
func evmMempoolJournalPath(appOpts servertypes.AppOptions) (path string, on bool) {
	on = cast.ToBool(appOpts.Get(evmMempoolJournalFlag))
	home := cast.ToString(appOpts.Get(flags.FlagHome))
	if !on || home == "" {
		return "", on
	}
	return filepath.Join(home, evmmempool.JournalDirName, evmmempool.JournalFileName), true
}

// setEVMMempool sets the EVM priority nonce mempool
// it is required for the ethereum json rpc server to work
//
// SA-M3 audit-2026-05-29: STOChain always runs with EVM enabled. If
// evmtypes.GetChainConfig() returns nil at this point, the EVM module was
// not initialized correctly — silent skip would result in:
//   - STOCProposalHandler not wired → Bug B PopCurrentAccount cascade-skip
//     missing → consensus failure on orphan EVM tx batches
//   - CheckTxHandler not set → orphan eviction inactive
//   - app.EVMMempool == nil → MaxPendingTxPerWalletDecorator no-op
// Fail loudly via panic so misconfiguration is caught at boot, not at first
// consensus failure under load.
func (app *App) setEVMMempool(appOpts servertypes.AppOptions) {
	// SA-M3 audit-2026-05-29: STOChain ALWAYS runs with EVM enabled. main's
	// upstream pattern (silent skip when ChainConfig == nil) would result in
	// STOCProposalHandler not wired (Bug B cascade-skip missing),
	// CheckTxHandler not set (orphan eviction inactive), and
	// MaxPendingTxPerWalletDecorator becoming a no-op. Fail loudly at boot
	// instead of silently degrading at first consensus failure under load.
	if evmtypes.GetChainConfig() == nil {
		panic("setEVMMempool: evmtypes.GetChainConfig() returned nil — EVM module not initialized; STOChain requires EVM enabled (STOCProposalHandler + orphan eviction + per-wallet cap would silently no-op)")
	}

	// NOTE: MaxTx limit is not available in EVMMempoolConfig for cosmos/evm v1.0.0-rc2.
	// DoS mitigation: minimum gas price (validator config) + ante handler gas validation.
	// BlockGasLimit 100M is standard for Cosmos EVM chains (similar to Evmos).
	// TODO: Add MaxTx when cosmos/evm supports it, or implement custom mempool wrapper.
	mempoolConfig := &evmmempool.EVMMempoolConfig{
		AnteHandler:   app.BaseApp.AnteHandler(),
		BlockGasLimit: 100_000_000,
		// STOChain fix: honour app.toml [evm.mempool] (price-limit, price-bump,
		// account/global slots and queue, lifetime). Without this the legacypool
		// always ran DefaultConfig (Lifetime 1h) whatever app.toml said. Unset or
		// zero values keep the defaults. Mempool config is node-local, not consensus.
		LegacyPoolConfig: evmconfig.GetLegacyPoolConfig(appOpts, app.Logger()),
		// STOChain fix: lets the mempool prime its pool right after a
		// restart, before the first new block (read-only query context).
		StartupContext: func() (sdk.Context, error) { return latestCommittedContext(app.BaseApp) },
	}
	// STOChain fix: optionally keep the txs held by this node's legacypool
	// across a graceful restart (written on Close, re-submitted through
	// CheckTx after start). Off unless app.toml sets evm.mempool.journal =
	// true. The file lives in <home>/evm-mempool/, never under <home>/data.
	// Node-local, not consensus.
	journalPath, journalOn := evmMempoolJournalPath(appOpts)
	switch {
	case journalPath != "":
		mempoolConfig.JournalPath = journalPath
		app.Logger().Info("EVM mempool journal enabled", "path", journalPath)
	case journalOn:
		app.Logger().Error("EVM mempool journal enabled but the node home is unknown; journal disabled")
	default:
		app.Logger().Info("EVM mempool journal disabled", "flag", evmMempoolJournalFlag)
	}

	// v0.6.0: cosmosPoolMaxTx=5000 limits pending Cosmos txs in EVM mempool (DoS mitigation)
	evmMempool := evmmempool.NewExperimentalEVMMempool(app.CreateQueryContext, app.Logger(), app.EVMKeeper, app.FeeMarketKeeper, app.txConfig, app.clientCtx, mempoolConfig, 5000)
	app.EVMMempool = evmMempool

	// Audit fix H1: boot-time sanity check. If EVMMempool is set but the
	// concrete type is not *ExperimentalEVMMempool, the GetEVMMempool getter
	// in MaxPendingTxPerWalletDecorator (app/ante/max_pending_tx.go) silently
	// returns nil and the per-wallet pending-tx cap becomes a no-op. Panic at
	// startup so the misconfiguration is surfaced before block production.
	if _, ok := app.EVMMempool.(*evmmempool.ExperimentalEVMMempool); !ok {
		panic(fmt.Sprintf(
			"app.EVMMempool wired but concrete type is %T, expected *evmmempool.ExperimentalEVMMempool; MaxPendingTxPerWalletDecorator would be a silent no-op",
			app.EVMMempool,
		))
	}

	app.SetMempool(evmMempool)
	checkTxHandler := evmmempool.NewCheckTxHandler(evmMempool)
	app.SetCheckTxHandler(checkTxHandler)

	// STOChain v8.1 (2026-05-17): wrap default proposal handler with
	// PopCurrentAccount cascade-skip. See app/abci_proposal.go for the
	// Bug B wire-up rationale.
	signerExt := evmmempool.NewEthSignerExtractionAdapter(sdkmempool.NewDefaultSignerExtractionAdapter())
	stocProposalHandler := NewSTOCProposalHandler(evmMempool, app, signerExt, app.Logger())
	app.SetPrepareProposal(stocProposalHandler.PrepareProposalHandler())
	app.SetProcessProposal(stocProposalHandler.ProcessProposalHandler())
}

// Close shuts the EVM mempool down before the base app closes its databases.
// STOChain fix: BaseApp.Close never closed the mempool, so its event-bus
// subscription and pool goroutines were never stopped and its tx journal
// (see setEVMMempool) could not be written on shutdown. The server start
// command calls this after CometBFT has stopped.
func (app *App) Close() error {
	var errs []error
	if m, ok := app.EVMMempool.(*evmmempool.ExperimentalEVMMempool); ok && m != nil {
		if err := m.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close EVM mempool: %w", err))
		}
	}
	if err := app.App.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RegisterPendingTxListener a function that registers a listener for pending transactions.
func (app *App) RegisterPendingTxListener(listener func(common.Hash)) {
	app.pendingTxListeners = append(app.pendingTxListeners, listener)
}

// SetClientCtx a function that sets the client context on the app, required by EVM module implementation.
func (app *App) SetClientCtx(ctx client.Context) {
	app.clientCtx = ctx
}

// GetMempool returns the mempool of the app.
// It is required by the EVM application interface.
// Returns the base app's mempool as ExtMempool fallback when EVM mempool is not initialized.
func (app *App) GetMempool() sdkmempool.ExtMempool {
	if app.EVMMempool != nil {
		return app.EVMMempool
	}
	// Fallback: try base app's mempool if it implements ExtMempool
	if mp, ok := app.BaseApp.Mempool().(sdkmempool.ExtMempool); ok {
		return mp
	}
	return nil
}

// getEVMChainID returns the EVM chain ID from the app options.
//
// Priority:
//  1. Explicit "evm.evm-chain-id" in app.toml (for existing chains that can't change their cosmos chain-id)
//  2. Parsed from cosmos chain-id format "name_EVMID-version" (e.g. stoc_1306-1)
//  3. Panics if chain-id cannot be parsed (requires explicit configuration to prevent chain ID collisions)
func getEVMChainID(appOpts servertypes.AppOptions) (uint64, error) {
	// Priority 1: explicit EVM chain ID from app.toml [evm] section
	// Key matches cosmos/evm config mapstructure tag: evm-chain-id under [evm] section
	if evmChainID := cast.ToUint64(appOpts.Get("evm.evm-chain-id")); evmChainID > 0 {
		return evmChainID, nil
	}

	chainID := cast.ToString(appOpts.Get(flags.FlagChainID))
	if chainID == "" {
		// fallback to genesis chain-id
		genesisPathCfg, _ := appOpts.Get("genesis_file").(string)
		if genesisPathCfg == "" {
			genesisPathCfg = filepath.Join("config", "genesis.json")
		}

		reader, err := os.Open(filepath.Join(DefaultNodeHome, genesisPathCfg))
		if err != nil {
			return 0, fmt.Errorf("failed to open genesis file: %w", err)
		}
		defer reader.Close()

		chainID, err = genutiltypes.ParseChainIDFromGenesis(reader)
		if err != nil {
			return 0, fmt.Errorf("failed to parse chain-id from genesis file: %w", err)
		}
	}

	return cosmosChainIDToEVMChainID(chainID)
}

// cosmosChainIDToEVMChainID converts a Cosmos chain ID to an EVM chain ID.
//
// Supports the canonical format:
//   - "name_EVMID-version" (e.g. stoc_1306-1) → extracts 1306
//
// SA-2026-06-02 MED-6 (senior-skeptic audit): the prior implementation
// accepted any number of leading zeros on EVMID via strconv.ParseUint,
// so "stoc_1306-1", "stoc_01306-1" and "stoc_000001306-1" all resolved
// to the same EVM chain ID. The Cosmos chain-id is byte-compared by
// CometBFT during consensus but the derived EVM chain ID drives EIP-155
// replay protection — a fork of one mainnet that re-uses the EVMID
// portion with extra leading zeros would silently inherit the parent's
// EIP-155 protection slot and replay every transaction. We now require
// the canonical form and reject any non-canonical encoding explicitly.
func cosmosChainIDToEVMChainID(chainID string) (uint64, error) {
	// Try to parse "name_EVMID-version" format (e.g. stoc_1306-1, stoc_1999-1)
	if idx := strings.LastIndex(chainID, "_"); idx != -1 {
		suffix := chainID[idx+1:] // "1306-1"
		evmPart := suffix
		if dashIdx := strings.Index(suffix, "-"); dashIdx != -1 {
			evmPart = suffix[:dashIdx] // "1306"
		}
		evmID, err := strconv.ParseUint(evmPart, 10, 64)
		if err == nil && evmID > 0 {
			// Reject leading zeros and any other non-canonical encoding
			// before accepting the chain ID. MED-6 rationale above.
			if strconv.FormatUint(evmID, 10) != evmPart {
				return 0, fmt.Errorf(
					"cosmos chain-id %q has non-canonical EVM chain ID %q (expected %q) — leading zeros are not allowed because they bypass EIP-155 replay protection slot aliasing",
					chainID, evmPart, strconv.FormatUint(evmID, 10),
				)
			}
			return evmID, nil
		}
	}

	return 0, fmt.Errorf(
		"cannot derive EVM chain ID from cosmos chain-id %q: expected format 'name_EVMID-version' (e.g. stoc_1306-1) "+
			"or set 'evm.evm-chain-id' in app.toml explicitly",
		chainID,
	)
}

// RegisterEVM Since the EVM modules don't support dependency injection,
// we need to manually register the modules on the client side.
// This needs to be removed after EVM supports App Wiring.
func RegisterEVM(cdc codec.Codec, interfaceRegistry codectypes.InterfaceRegistry) map[string]appmodule.AppModule {
	modules := map[string]appmodule.AppModule{
		evmtypes.ModuleName:       vm.NewAppModule(nil, authkeeper.AccountKeeper{}, nil, interfaceRegistry.SigningContext().AddressCodec()),
		erc20types.ModuleName:     erc20.NewAppModule(erc20keeper.Keeper{}, authkeeper.AccountKeeper{}),
		feemarkettypes.ModuleName: feemarket.NewAppModule(feemarketkeeper.Keeper{}),
	}

	for _, m := range modules {
		if mr, ok := m.(module.AppModuleBasic); ok {
			mr.RegisterInterfaces(cdc.InterfaceRegistry())
		}
	}

	return modules
}

// ProvideMsgEthereumTxCustomGetSigner provides a custom signer for the MsgEthereumTx message.
func ProvideMsgEthereumTxCustomGetSigner() signing.CustomGetSigner {
	return evmtypes.MsgEthereumTxCustomGetSigner
}
