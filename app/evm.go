package app

import (
	"context"
	"fmt"
	"maps"
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
	gethvm "github.com/ethereum/go-ethereum/core/vm"

	evmutil "stoc/x/evmutil"
	evmutilkeeper "stoc/x/evmutil/keeper"
	evmutiltypes "stoc/x/evmutil/types"
)

// registerEVMModules register EVM keepers and non dependency inject modules.
func (app *App) registerEVMModules(appOpts servertypes.AppOptions) error {
	// chain config
	chainID, err := getEVMChainID(appOpts)
	if err != nil {
		return fmt.Errorf("failed to get EVM chain ID: %w", err)
	}
	coinInfoMap := map[uint64]evmtypes.EvmCoinInfo{
		chainID: evmtypes.EvmCoinInfo{
			Denom:         sdk.DefaultBondDenom,                       // "ustoc" — base cosmos denom for fee payment
			ExtendedDenom: evmutiltypes.GetEvmDenom(),                 // "astoc" — 18-decimal EVM representation
			DisplayDenom:  strings.TrimPrefix(sdk.DefaultBondDenom, "u"), // "stoc" — human-readable display name
			Decimals:      evmtypes.SixDecimals,                       // 6 decimals — cosmos/evm applies ConversionFactor(10^12) internally
		},
	}

	// configure evm modules
	if err := evmconfig.EvmAppOptionsWithConfig(
		chainID,
		coinInfoMap,
		getCustomEVMActivators(),
	); err != nil {
		return err
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
		tracer,
	)

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
		vm.NewAppModule(app.EVMKeeper, app.AccountKeeper, app.AccountKeeper.AddressCodec()),
		feemarket.NewAppModule(app.FeeMarketKeeper),
		erc20.NewAppModule(app.Erc20Keeper, app.AccountKeeper),
	); err != nil {
		return err
	}

	return nil
}

func (app *App) postRegisterEVMModules() error {
	// register precompiles on EVMKeeper
	const bech32PrecompileBaseGas = 6_000

	// secp256r1 precompile as per EIP-7212
	p256Precompile := &p256.Precompile{}

	bech32Precompile, err := bech32.NewPrecompile(bech32PrecompileBaseGas)
	if err != nil {
		return fmt.Errorf("failed to instantiate bech32 precompile: %w", err)
	}

	// Safe to use maps.Clone: WithStaticPrecompiles uses map lookups (not iteration) in consensus paths
	precompiles := maps.Clone(gethvm.PrecompiledContractsPrague)
	precompiles[bech32Precompile.Address()] = bech32Precompile
	precompiles[p256Precompile.Address()] = p256Precompile

	// add more stateful precompiles here, if needed.

	app.EVMKeeper.WithStaticPrecompiles(precompiles)

	// Build precompile address blocklist for bank SendRestriction
	blockedPrecompiles := make(map[string]bool, len(precompiles))
	for addr := range precompiles {
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

// setEVMMempool sets the EVM priority nonce mempool
// it is required for the ethereum json rpc server to work
func (app *App) setEVMMempool() {
	if evmtypes.GetChainConfig() != nil {
		// NOTE: MaxTx limit is not available in EVMMempoolConfig for this EVM version
		// Consider implementing custom mempool wrapper or upgrading EVM module for DoS protection
		// Current mitigation: Rely on minimum gas price and ante handler validation
		mempoolConfig := &evmmempool.EVMMempoolConfig{
			AnteHandler:   app.BaseApp.AnteHandler(),
			BlockGasLimit: 100_000_000,
		}

		evmMempool := evmmempool.NewExperimentalEVMMempool(app.CreateQueryContext, app.Logger(), app.EVMKeeper, app.FeeMarketKeeper, app.txConfig, app.clientCtx, mempoolConfig)
		app.EVMMempool = evmMempool

		app.SetMempool(evmMempool)
		checkTxHandler := evmmempool.NewCheckTxHandler(evmMempool)
		app.SetCheckTxHandler(checkTxHandler)

		abciProposalHandler := baseapp.NewDefaultProposalHandler(evmMempool, app)
		abciProposalHandler.SetSignerExtractionAdapter(evmmempool.NewEthSignerExtractionAdapter(sdkmempool.NewDefaultSignerExtractionAdapter()))
		app.SetPrepareProposal(abciProposalHandler.PrepareProposalHandler())
		app.SetProcessProposal(abciProposalHandler.ProcessProposalHandler())
	}
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
func (app *App) GetMempool() sdkmempool.ExtMempool {
	return app.EVMMempool
}

// Custom EVM activator IDs for gas multiplier overrides.
const (
	activatorCreateGasMultiplier = 0 // Multiplies CREATE/CREATE2 constant gas by 10x
	activatorCallGasMultiplier   = 1 // Multiplies CALL constant gas by 10x
	activatorSstoreFixedGas      = 2 // Sets SSTORE constant gas to 2100 (EIP-2929 warm access cost)
)

// getCustomEVMActivators defines a map of opcode modifiers associated
// with a key defining the corresponding activator ID.
func getCustomEVMActivators() map[int]func(*gethvm.JumpTable) {
	var (
		multiplier        = uint64(10)
		sstoreConstantGas = uint64(2100) // EIP-2929 warm access cost — prevents state bloat from cheap storage writes
	)

	return map[int]func(*gethvm.JumpTable){
		activatorCreateGasMultiplier: func(jt *gethvm.JumpTable) {
			currentValCreate := jt[gethvm.CREATE].GetConstantGas()
			jt[gethvm.CREATE].SetConstantGas(currentValCreate * multiplier)

			currentValCreate2 := jt[gethvm.CREATE2].GetConstantGas()
			jt[gethvm.CREATE2].SetConstantGas(currentValCreate2 * multiplier)
		},
		activatorCallGasMultiplier: func(jt *gethvm.JumpTable) {
			currentVal := jt[gethvm.CALL].GetConstantGas()
			jt[gethvm.CALL].SetConstantGas(currentVal * multiplier)
		},
		activatorSstoreFixedGas: func(jt *gethvm.JumpTable) {
			jt[gethvm.SSTORE].SetConstantGas(sstoreConstantGas)
		},
	}
}

// getEVMChainID returns the EVM chain ID from the app options.
//
// Priority:
//  1. Explicit "evm.evm-chain-id" in app.toml (for existing chains that can't change their cosmos chain-id)
//  2. Parsed from cosmos chain-id format "name_EVMID-version" (e.g. stoc_1306-1)
//  3. FNV-1a hash of cosmos chain-id string (fallback)
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

	return cosmosChainIDToEVMChainID(chainID), nil
}

// cosmosChainIDToEVMChainID converts a Cosmos chain ID to an EVM chain ID.
//
// Supports two formats:
//   - "name_EVMID-version" (e.g. stoc_1306-1) → extracts 1306
//   - Any other string → FNV-1a hash (backward compatible)
func cosmosChainIDToEVMChainID(chainID string) uint64 {
	// Try to parse "name_EVMID-version" format (e.g. stoc_1306-1, stoc_1999-1)
	if idx := strings.LastIndex(chainID, "_"); idx != -1 {
		suffix := chainID[idx+1:] // "1306-1"
		evmPart := suffix
		if dashIdx := strings.Index(suffix, "-"); dashIdx != -1 {
			evmPart = suffix[:dashIdx] // "1306"
		}
		if evmID, err := strconv.ParseUint(evmPart, 10, 64); err == nil && evmID > 0 {
			return evmID
		}
	}

	// No fallback — require explicit configuration to prevent chain ID collisions.
	// A colliding EVM chain ID enables cross-chain transaction replay attacks.
	panic(fmt.Sprintf(
		"cannot derive EVM chain ID from cosmos chain-id %q: expected format 'name_EVMID-version' (e.g. stoc_1306-1) "+
			"or set 'evm.evm-chain-id' in app.toml explicitly",
		chainID,
	))
}

// RegisterEVM Since the EVM modules don't support dependency injection,
// we need to manually register the modules on the client side.
// This needs to be removed after EVM supports App Wiring.
func RegisterEVM(cdc codec.Codec, interfaceRegistry codectypes.InterfaceRegistry) map[string]appmodule.AppModule {
	modules := map[string]appmodule.AppModule{
		evmtypes.ModuleName:       vm.NewAppModule(nil, authkeeper.AccountKeeper{}, interfaceRegistry.SigningContext().AddressCodec()),
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
