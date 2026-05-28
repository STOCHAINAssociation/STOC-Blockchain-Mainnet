package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "cosmossdk.io/api/cosmos/tx/config/v1" // import for side-effects
	clienthelpers "cosmossdk.io/client/v2/helpers"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	_ "cosmossdk.io/x/circuit" // import for side-effects
	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	circuittypes "cosmossdk.io/x/circuit/types"
	_ "cosmossdk.io/x/evidence" // import for side-effects
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	_ "cosmossdk.io/x/feegrant/module" // import for side-effects
	"cosmossdk.io/x/nft"
	nftkeeper "cosmossdk.io/x/nft/keeper"
	_ "cosmossdk.io/x/nft/module" // import for side-effects
	_ "cosmossdk.io/x/upgrade"    // import for side-effects
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cast"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	_ "github.com/cosmos/cosmos-sdk/x/auth/tx/config" // import for side-effects
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	_ "github.com/cosmos/cosmos-sdk/x/auth/vesting" // import for side-effects
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	_ "github.com/cosmos/cosmos-sdk/x/authz/module" // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/bank"         // import for side-effects
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	_ "github.com/cosmos/cosmos-sdk/x/consensus" // import for side-effects
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	_ "github.com/cosmos/cosmos-sdk/x/crisis" // import for side-effects
	crisiskeeper "github.com/cosmos/cosmos-sdk/x/crisis/keeper"
	_ "github.com/cosmos/cosmos-sdk/x/distribution" // import for side-effects
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govclient "github.com/cosmos/cosmos-sdk/x/gov/client"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	_ "github.com/cosmos/cosmos-sdk/x/group/module" // import for side-effects
	_ "github.com/cosmos/cosmos-sdk/x/mint"         // import for side-effects
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	_ "github.com/cosmos/cosmos-sdk/x/params" // import for side-effects
	paramsclient "github.com/cosmos/cosmos-sdk/x/params/client"
	paramskeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	_ "github.com/cosmos/cosmos-sdk/x/slashing" // import for side-effects
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	_ "github.com/cosmos/cosmos-sdk/x/staking" // import for side-effects
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	vestingexported "github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
	_ "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts" // import for side-effects
	icacontrollerkeeper "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/controller/keeper"
	icahostkeeper "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/host/keeper"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransferkeeper "github.com/cosmos/ibc-go/v10/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	stocmodulekeeper "stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
	// this line is used by starport scaffolding # stargate/app/moduleImport

	"stoc/docs"
	stocante "stoc/x/stoc/ante"

	// EVM imports
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	evmante "github.com/cosmos/evm/ante"
	antetypes "github.com/cosmos/evm/ante/types"
	evmmempool "github.com/cosmos/evm/mempool"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	"github.com/ethereum/go-ethereum/common"
	stocappante "stoc/app/ante"
	evmutilkeeper "stoc/x/evmutil/keeper"
)

const (
	AccountAddressPrefix = "stoc"
	Name                 = "stoc"
	// ChainCoinType is the coin type for stoc chain
	// Using 118 (Cosmos standard) for backward compatibility with existing accounts
	ChainCoinType = 118
)

var (
	// DefaultNodeHome default home directories for the application daemon
	DefaultNodeHome string
)

var (
	_ runtime.AppI            = (*App)(nil)
	_ servertypes.Application = (*App)(nil)
)

// App extends an ABCI application, but with most of its parameters exported.
// They are exported for convenience in creating helper functions, as object
// capabilities aren't needed for testing.
type App struct {
	*runtime.App
	legacyAmino       *codec.LegacyAmino
	appCodec          codec.Codec
	txConfig          client.TxConfig
	interfaceRegistry codectypes.InterfaceRegistry

	// keepers
	AccountKeeper         authkeeper.AccountKeeper
	BankKeeper            bankkeeper.Keeper
	StakingKeeper         *stakingkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	ConsensusParamsKeeper consensuskeeper.Keeper

	SlashingKeeper       slashingkeeper.Keeper
	MintKeeper           mintkeeper.Keeper
	GovKeeper            *govkeeper.Keeper
	CrisisKeeper         *crisiskeeper.Keeper
	UpgradeKeeper        *upgradekeeper.Keeper
	ParamsKeeper         paramskeeper.Keeper
	AuthzKeeper          authzkeeper.Keeper
	EvidenceKeeper       evidencekeeper.Keeper
	FeeGrantKeeper       feegrantkeeper.Keeper
	GroupKeeper          groupkeeper.Keeper
	NFTKeeper            nftkeeper.Keeper
	CircuitBreakerKeeper circuitkeeper.Keeper

	// IBC
	IBCKeeper           *ibckeeper.Keeper // IBC Keeper must be a pointer in the app, so we can SetRouter on it correctly
	ICAControllerKeeper icacontrollerkeeper.Keeper
	ICAHostKeeper       icahostkeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper

	// EVM Keepers
	EVMKeeper         *evmkeeper.Keeper
	FeeMarketKeeper   feemarketkeeper.Keeper
	Erc20Keeper       erc20keeper.Keeper
	EvmutilKeeper     evmutilkeeper.Keeper

	// EVM mempool and client context
	EVMMempool         sdkmempool.ExtMempool
	clientCtx          client.Context
	pendingTxListeners []func(common.Hash)

	StocKeeper stocmodulekeeper.Keeper
	// this line is used by starport scaffolding # stargate/app/keeperDeclaration

	// blockedPrecompileAddrs contains bech32 addresses of EVM precompiles.
	// Used by blockPrecompileTransfers to prevent direct Cosmos sends to precompile addresses.
	blockedPrecompileAddrs map[string]bool

	// simulation manager
	sm *module.SimulationManager
}

func init() {
	var err error
	clienthelpers.EnvPrefix = Name
	DefaultNodeHome, err = clienthelpers.GetNodeHomeDirectory("." + Name)
	if err != nil {
		panic(err)
	}

	// Set default bond denom from genesis staking params.
	// Must be set before EVM modules use it (evm.go coinInfoMap, evmutil GetEvmDenom()).
	// Reads genesis to support both mainnet (ustoc) and testnet (utstoc).
	sdk.DefaultBondDenom = detectBondDenomFromGenesis(DefaultNodeHome)
}

// detectBondDenomFromGenesis reads bond_denom from genesis file.
// For fresh nodes (no genesis yet), derives denom from chain-id in config.toml
// or falls back based on DefaultNodeHome convention.
// Supports: mainnet "ustoc", testnet "utstoc"
func detectBondDenomFromGenesis(homeDir string) string {
	// Try genesis first (most reliable source)
	genesisPath := filepath.Join(homeDir, "config", "genesis.json")
	data, err := os.ReadFile(genesisPath)
	if err == nil {
		var genesis struct {
			AppState struct {
				Staking struct {
					Params struct {
						BondDenom string `json:"bond_denom"`
					} `json:"params"`
				} `json:"staking"`
			} `json:"app_state"`
		}
		if err := json.Unmarshal(data, &genesis); err == nil {
			if genesis.AppState.Staking.Params.BondDenom != "" {
				return genesis.AppState.Staking.Params.BondDenom
			}
		}
	}

	// No genesis yet (fresh node, e.g. ignite chain serve).
	// Default to mainnet denom — Ignite will generate genesis with the correct denom shortly.
	// Previous heuristic (scanning config.toml for "tstoc"/"testnet") was removed because
	// substring matching could trigger on unrelated content (moniker, comments), causing
	// a mainnet node to use the wrong denom.
	return "ustoc"
}

// getGovProposalHandlers return the chain proposal handlers.
func getGovProposalHandlers() []govclient.ProposalHandler {
	var govProposalHandlers []govclient.ProposalHandler
	// this line is used by starport scaffolding # stargate/app/govProposalHandlers

	govProposalHandlers = append(govProposalHandlers,
		paramsclient.ProposalHandler,
		// this line is used by starport scaffolding # stargate/app/govProposalHandler
	)

	return govProposalHandlers
}

// AppConfig returns the default app config.
func AppConfig() depinject.Config {
	return depinject.Configs(
		appConfig,
		// Alternatively, load the app config from a YAML file.
		// appconfig.LoadYAML(AppConfigYAML),
		depinject.Supply(
			// supply custom module basics
			map[string]module.AppModuleBasic{
				genutiltypes.ModuleName: genutil.NewAppModuleBasic(genutiltypes.DefaultMessageValidator),
				govtypes.ModuleName:     gov.NewAppModuleBasic(getGovProposalHandlers()),
				authtypes.ModuleName:    auth.AppModuleBasic{},
				// this line is used by starport scaffolding # stargate/appConfig/moduleBasic
			},
		),
	)
}

// New returns a reference to an initialized App.
func New(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) (*App, error) {
	var (
		app        = &App{}
		appBuilder *runtime.AppBuilder

		// merge the AppConfig and other configuration in one config
		appConfig = depinject.Configs(
			AppConfig(),
			depinject.Supply(
				appOpts, // supply app options
				logger,  // supply logger
				// Supply with IBC keeper getter for the IBC modules with App Wiring.
				// The IBC Keeper cannot be passed because it has not been initiated yet.
				// Passing the getter, the app IBC Keeper will always be accessible.
				// This needs to be removed after IBC supports App Wiring.
				app.GetIBCKeeper,
				module.NewManager(
					auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
				),
				[]string{
					authtypes.ModuleName,
					banktypes.ModuleName,
					stakingtypes.ModuleName,
					minttypes.ModuleName,
					distrtypes.ModuleName,
					govtypes.ModuleName,
					paramstypes.ModuleName,
					ibcexported.ModuleName,
					upgradetypes.ModuleName,
					evidencetypes.ModuleName,
					ibctransfertypes.ModuleName,
					icatypes.ModuleName,
					group.ModuleName,
					feegrant.ModuleName,
					nft.ModuleName,
					circuittypes.ModuleName,
				},
			), depinject.Provide(ProvideMsgEthereumTxCustomGetSigner),
		)
	)

	if err := depinject.Inject(appConfig,
		&appBuilder,
		&app.appCodec,
		&app.legacyAmino,
		&app.txConfig,
		&app.interfaceRegistry,
		&app.AccountKeeper,
		&app.BankKeeper,
		&app.StakingKeeper,
		&app.DistrKeeper,
		&app.ConsensusParamsKeeper,
		&app.SlashingKeeper,
		&app.MintKeeper,
		&app.GovKeeper,
		&app.CrisisKeeper,
		&app.UpgradeKeeper,
		&app.ParamsKeeper,
		&app.AuthzKeeper,
		&app.EvidenceKeeper,
		&app.FeeGrantKeeper,
		&app.NFTKeeper,
		&app.GroupKeeper,
		&app.CircuitBreakerKeeper,
		&app.StocKeeper,
		// this line is used by starport scaffolding # stargate/app/keeperDefinition
	); err != nil {
		panic(err)
	}

	// Optimistic execution disabled — experimental SDK feature that risks state divergence
	// when combined with state-modifying PostHandler (tax). Re-enable when SDK matures.
	// baseAppOptions = append(baseAppOptions, baseapp.SetOptimisticExecution())

	// build app
	app.App = appBuilder.Build(db, traceStore, baseAppOptions...)

	// register legacy modules
	if err := app.registerIBCModules(appOpts); err != nil {
		return nil, err
	}

	// Register EVM modules
	if err := app.registerEVMModules(appOpts); err != nil {
		return nil, err
	}

	// Post-register EVM modules (precompiles)
	if err := app.postRegisterEVMModules(); err != nil {
		return nil, err
	}

	// Block transfers to EVM precompile addresses.
	// NOTE: BlockedModuleAccountsOverride only handles module names (via NewModuleAddress),
	// so precompile hex addresses must be blocked via SendRestriction instead.
	app.blockPrecompileTransfers()

	// Block custom token IBC transfers at the bank module level.
	// This is the primary enforcement — catches ALL execution paths including
	// ICA host, x/group proposals, and x/gov proposals that bypass ante handlers.
	// The ante handler (IBCCustomTokenRestriction) remains for early rejection and user-facing errors.
	app.blockCustomTokenIBCTransfers()

	// Block custom token sends to/from non-wallet accounts (ICA, vesting, non-stoc modules).
	// Defense-in-depth for Q2 + Q3b: custom tokens are company securities and may only
	// move wallet-to-wallet. This SendRestriction catches execution paths that bypass
	// the ante chain (ICA host, group/gov proposal execution) where the ante-level
	// CustomTokenChainOpsRestriction cannot see inner messages.
	app.blockCustomTokenNonWalletAccounts()

	// register streaming services
	if err := app.RegisterStreamingServices(appOpts, app.kvStoreKeys()); err != nil {
		return nil, err
	}

	/****  Module Options ****/

	app.ModuleManager.RegisterInvariants(app.CrisisKeeper)

	// create the simulation manager and define the order of the modules for deterministic simulations
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AccountKeeper, authsims.RandomGenesisAccounts, app.GetSubspace(authtypes.ModuleName)),
	}
	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, overrideModules)
	app.sm.RegisterStoreDecoders()

	// A custom InitChainer sets if extra pre-init-genesis logic is required.
	// This is necessary for manually registered modules that do not support app wiring.
	// Manually set the module version map as shown below.
	// The upgrade module will automatically handle de-duplication of the module version map.
	app.SetInitChainer(func(ctx sdk.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
		if err := app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()); err != nil {
			return nil, err
		}
		return app.App.InitChainer(ctx, req)
	})

	// create options for AnteHandler
	// MaxTxGasWanted caps per-tx gas to prevent single-tx block monopolization.
	// Default 50M = half of BlockGasLimit (100M), allowing other txs in the same block.
	maxGasWanted := cast.ToUint64(appOpts.Get("gas-wanted"))
	if maxGasWanted == 0 {
		maxGasWanted = 50_000_000
	}

	anteOptions := stocappante.StocAnteOptions{
		HandlerOptions: evmante.HandlerOptions{
			Cdc:                    app.appCodec,
			AccountKeeper:          app.AccountKeeper,
			BankKeeper:             app.BankKeeper,
			SignModeHandler:        app.txConfig.SignModeHandler(),
			FeegrantKeeper:         app.FeeGrantKeeper,
			SigGasConsumer:         evmante.SigVerificationGasConsumer,
			ExtensionOptionChecker: antetypes.HasDynamicFeeExtensionOption,
			DynamicFeeChecker:      true, // v0.6.0: replaces TxFeeChecker with boolean flag
			EvmKeeper:              app.EVMKeeper,
			FeeMarketKeeper:        &app.FeeMarketKeeper,
			MaxTxGasWanted:         maxGasWanted,
			PendingTxListener: func(hash common.Hash) {
				for _, listener := range app.pendingTxListeners {
					listener(hash)
				}
			},
			IBCKeeper: app.IBCKeeper,
		},
		StocKeeper: app.StocKeeper,
		// Late-binding getter for the ExperimentalEVMMempool. The mempool is
		// constructed below by setEVMMempool() after SetAnteHandler. The
		// decorator queries it lazily at CheckTx time via this closure.
		GetEVMMempool: func() *evmmempool.ExperimentalEVMMempool {
			if app.EVMMempool == nil {
				return nil
			}
			m, _ := app.EVMMempool.(*evmmempool.ExperimentalEVMMempool)
			return m
		},
		MaxPendingTxPerWallet: stocappante.DefaultMaxPendingTxPerWallet,
	}
	if err := anteOptions.Validate(); err != nil {
		panic(err)
	}

	anteHandler := stocappante.NewAnteHandler(anteOptions)
	app.SetAnteHandler(anteHandler)

	// Setup EVM mempool
	app.setEVMMempool()

	postHandler := stocante.NewTaxPostDecorator(app.StocKeeper, app.appCodec)
	app.SetPostHandler(sdk.ChainPostDecorators(postHandler))

	// Register upgrade handlers for EVM migration
	app.RegisterUpgradeHandlers()

	if err := app.Load(loadLatest); err != nil {
		return nil, err
	}

	return app, nil
}

// LegacyAmino returns App's amino codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *App) LegacyAmino() *codec.LegacyAmino {
	return app.legacyAmino
}

// AppCodec returns App's app codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *App) AppCodec() codec.Codec {
	return app.appCodec
}

// InterfaceRegistry returns App's interfaceRegistry.
func (app *App) InterfaceRegistry() codectypes.InterfaceRegistry {
	return app.interfaceRegistry
}

// TxConfig returns App's tx config.
func (app *App) TxConfig() client.TxConfig {
	return app.txConfig
}

// GetKey returns the KVStoreKey for the provided store key.
func (app *App) GetKey(storeKey string) *storetypes.KVStoreKey {
	kvStoreKey, ok := app.UnsafeFindStoreKey(storeKey).(*storetypes.KVStoreKey)
	if !ok {
		return nil
	}
	return kvStoreKey
}

// GetMemKey returns the MemoryStoreKey for the provided store key.
func (app *App) GetMemKey(storeKey string) *storetypes.MemoryStoreKey {
	key, ok := app.UnsafeFindStoreKey(storeKey).(*storetypes.MemoryStoreKey)
	if !ok {
		return nil
	}

	return key
}

// kvStoreKeys returns all the kv store keys registered inside App.
func (app *App) kvStoreKeys() map[string]*storetypes.KVStoreKey {
	return app.GetStoreKeysMap()
}

// GetStoreKeysMap returns all the kv store keys registered inside App as a map.
func (app *App) GetStoreKeysMap() map[string]*storetypes.KVStoreKey {
	storeKeysMap := make(map[string]*storetypes.KVStoreKey)
	for _, storeKey := range app.GetStoreKeys() {
		kvStoreKey, ok := app.UnsafeFindStoreKey(storeKey.Name()).(*storetypes.KVStoreKey)
		if ok {
			storeKeysMap[storeKey.Name()] = kvStoreKey
		}
	}
	return storeKeysMap
}

// GetSubspace returns a param subspace for a given module name.
func (app *App) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// GetIBCKeeper returns the IBC keeper.
func (app *App) GetIBCKeeper() *ibckeeper.Keeper {
	return app.IBCKeeper
}


// SimulationManager implements the SimulationApp interface.
func (app *App) SimulationManager() *module.SimulationManager {
	return app.sm
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *App) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	app.App.RegisterAPIRoutes(apiSvr, apiConfig)
	// register swagger API in app.go so that other applications can override easily
	if err := server.RegisterSwaggerAPI(apiSvr.ClientCtx, apiSvr.Router, apiConfig.Swagger); err != nil {
		panic(err)
	}

	// register app's OpenAPI routes.
	docs.RegisterOpenAPIService(Name, apiSvr.Router)
}

// GetMaccPerms returns a copy of the module account permissions
//
// NOTE: This is solely to be used for testing purposes.
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for _, perms := range moduleAccPerms {
		dup[perms.Account] = perms.Permissions
	}
	return dup
}

// blockCustomTokenIBCTransfers registers a bank SendRestriction that prevents
// custom tokens (created via x/stoc) from being escrowed by IBC transfer.
// Unlike the ante handler (which only catches user-submitted txs), this restriction
// is enforced at the bank module level and catches ALL execution paths:
// - Direct user MsgTransfer
// - ICA host message execution (bypasses ante handlers)
// - x/group proposal execution (bypasses ante handlers)
// - x/gov proposal execution (bypasses ante handlers)
func (app *App) blockCustomTokenIBCTransfers() {
	stocKeeper := app.StocKeeper
	ibcKeeper := app.IBCKeeper

	// Cached escrow addresses with mutex protection.
	// SendRestriction is called from both DeliverTx (serial) and CheckTx (concurrent),
	// so the cache MUST be protected against concurrent access.
	var mu sync.RWMutex
	var escrowAddrs map[string]string // bech32 addr → channelId
	var cacheHeight int64

	app.BankKeeper.AppendSendRestriction(func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Fast path: skip if no custom tokens in the amount
		var customDenom string
		for _, coin := range amt {
			if stoctypes.IsNativeDenom(coin.Denom) {
				continue
			}
			if stocKeeper.HasToken(sdkCtx, coin.Denom) {
				customDenom = coin.Denom
				break
			}
		}
		if customDenom == "" {
			return toAddr, nil
		}

		// Rebuild escrow address cache every 10 blocks to pick up new channels
		currentHeight := sdkCtx.BlockHeight()
		mu.RLock()
		needRebuild := escrowAddrs == nil || currentHeight-cacheHeight >= 10
		mu.RUnlock()

		if needRebuild {
			mu.Lock()
			// Double-check after acquiring write lock (must match read-lock threshold)
			if escrowAddrs == nil || currentHeight-cacheHeight >= 10 {
				newCache := make(map[string]string)
				channels := ibcKeeper.ChannelKeeper.GetAllChannels(sdkCtx)
				for _, ch := range channels {
					if ch.PortId == ibctransfertypes.PortID {
						addr := ibctransfertypes.GetEscrowAddress(ch.PortId, ch.ChannelId)
						newCache[addr.String()] = ch.ChannelId
					}
				}
				escrowAddrs = newCache
				cacheHeight = currentHeight
			}
			mu.Unlock()
		}

		// O(1) lookup with read lock
		mu.RLock()
		channelId, blocked := escrowAddrs[toAddr.String()]
		mu.RUnlock()

		if blocked {
			sdkCtx.Logger().Warn("Blocked IBC escrow of custom token via SendRestriction",
				"denom", customDenom,
				"channel", channelId,
				"from", fromAddr.String(),
			)
			return toAddr, fmt.Errorf(
				"IBC transfer of custom token %q is not allowed: custom tokens created via x/stoc are Cosmos-only and cannot be transferred cross-chain",
				customDenom,
			)
		}

		return toAddr, nil
	})
}

// blockCustomTokenNonWalletAccounts registers a bank SendRestriction that
// prevents custom stoc tokens from being sent to or from any account that is
// not a regular user wallet. Specifically blocks:
//
//   - *icatypes.InterchainAccount — prevents Q3b tax bypass via ICA host
//     dispatching inner bank.MsgSend without going through the ante chain
//   - vestingexported.VestingAccount (ContinuousVestingAccount,
//     DelayedVestingAccount, PeriodicVestingAccount, PermanentLockedAccount)
//     — prevents Q2 Scenario 1 tax bypass even when ante chain is bypassed
//   - *authtypes.ModuleAccount (except x/stoc itself) — prevents Q2 Scenarios
//     2/3 tax bypass even when ante chain is bypassed (gov proposal execution,
//     group proposal execution)
//
// The x/stoc module account is explicitly ALLOWED as source or destination
// because the legitimate token lifecycle needs it:
//   - CreateToken: MintCoins(stoc_module) then SendCoinsFromModuleToAccount(stoc_module → creator)
//   - ReleaseTokens: SendCoinsFromModuleToAccount(stoc_module → recipient)
//   - BurnToken: SendCoinsFromAccountToModule(user → stoc_module) then BurnCoins(stoc_module)
//
// Native chain denoms (ustoc/astoc/stoc + test/devnet variants) are NEVER
// affected by this restriction — staking STOC, vesting STOC for team/payroll,
// gov deposit in STOC, community pool in STOC are all legitimate and pass through.
//
// This complements the CustomTokenChainOpsRestriction ante decorator:
//   - Ante layer: fast rejection with clear user-facing errors for known msg types
//   - Bank layer: defense-in-depth catching execution paths that bypass ante
//     (ICA host, group.MsgExec, gov proposal execution)
func (app *App) blockCustomTokenNonWalletAccounts() {
	stocKeeper := app.StocKeeper
	accountKeeper := app.AccountKeeper
	stocModuleAddr := authtypes.NewModuleAddress(stoctypes.ModuleName)

	app.BankKeeper.AppendSendRestriction(func(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) (sdk.AccAddress, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)

		// Fast path: skip if no custom tokens in the amount
		var customDenom string
		for _, coin := range amt {
			if stoctypes.IsNativeDenom(coin.Denom) {
				continue
			}
			if stocKeeper.HasToken(sdkCtx, coin.Denom) {
				customDenom = coin.Denom
				break
			}
		}
		if customDenom == "" {
			return toAddr, nil
		}

		// Allow x/stoc module as source or destination — required for legitimate
		// token lifecycle operations (CreateToken mint distribution, ReleaseTokens,
		// BurnToken collection). All other module accounts are blocked.
		fromIsStoc := fromAddr.Equals(stocModuleAddr)
		toIsStoc := toAddr.Equals(stocModuleAddr)

		// Check fromAddr account type (skip if it's the stoc module)
		if !fromIsStoc {
			if err := rejectIfNonWallet(sdkCtx, accountKeeper, fromAddr, customDenom, "from"); err != nil {
				return toAddr, err
			}
		}

		// Check toAddr account type (skip if it's the stoc module)
		if !toIsStoc {
			if err := rejectIfNonWallet(sdkCtx, accountKeeper, toAddr, customDenom, "to"); err != nil {
				return toAddr, err
			}
		}

		return toAddr, nil
	})
}

// rejectIfNonWallet returns an error if the given account is any of:
// InterchainAccount, VestingAccount, or ModuleAccount. Returns nil for regular
// BaseAccount, EthAccount, or nil (new account not yet created) — all treated
// as user wallets.
func rejectIfNonWallet(ctx sdk.Context, ak authkeeper.AccountKeeper, addr sdk.AccAddress, denom, direction string) error {
	acc := ak.GetAccount(ctx, addr)
	if acc == nil {
		// New account (first-time receive) — treat as regular wallet
		return nil
	}

	// Check InterchainAccount (ICA host-controlled account)
	if _, isICA := acc.(*icatypes.InterchainAccount); isICA {
		ctx.Logger().Warn("Blocked custom token send involving ICA account",
			"denom", denom, "direction", direction, "addr", addr.String(),
		)
		return fmt.Errorf(
			"custom token %q cannot be sent %s interchain account %s: custom stoc tokens may only move between regular user wallets",
			denom, direction, addr.String(),
		)
	}

	// Check VestingAccount (all 4 types satisfy this interface)
	if _, isVesting := acc.(vestingexported.VestingAccount); isVesting {
		ctx.Logger().Warn("Blocked custom token send involving vesting account",
			"denom", denom, "direction", direction, "addr", addr.String(),
		)
		return fmt.Errorf(
			"custom token %q cannot be sent %s vesting account %s: custom stoc tokens may only move between regular user wallets",
			denom, direction, addr.String(),
		)
	}

	// Check ModuleAccount (community pool, gov, distribution, feegrant, etc.)
	// x/stoc module was already allowed above before calling this function.
	if _, isModule := acc.(sdk.ModuleAccountI); isModule {
		ctx.Logger().Warn("Blocked custom token send involving non-stoc module account",
			"denom", denom, "direction", direction, "addr", addr.String(),
		)
		return fmt.Errorf(
			"custom token %q cannot be sent %s module account %s: custom stoc tokens may only move between regular user wallets (x/stoc module excepted for token lifecycle)",
			denom, direction, addr.String(),
		)
	}

	return nil
}

// DefaultGenesis returns default genesis state with STOC-specific EVM denom configuration.
// Overrides cosmos/evm defaults ("aatom") with denoms derived from bond_denom.
func (app *App) DefaultGenesis() map[string]json.RawMessage {
	genesis := app.App.DefaultGenesis()

	// Override EVM module genesis: set correct denoms for 6-decimal chain
	bondDenom := sdk.DefaultBondDenom
	extendedDenom := "a" + bondDenom[1:] // "ustoc" → "astoc"

	evmGenState := evmtypes.DefaultGenesisState()
	evmGenState.Params.EvmDenom = bondDenom
	evmGenState.Params.ExtendedDenomOptions = &evmtypes.ExtendedDenomOptions{
		ExtendedDenom: extendedDenom,
	}
	evmGenState.Params.ActiveStaticPrecompiles = evmtypes.AvailableStaticPrecompiles
	evmGenState.Preinstalls = evmtypes.DefaultPreinstalls
	genesis[evmtypes.ModuleName] = app.appCodec.MustMarshalJSON(evmGenState)

	return genesis
}

// BlockedAddresses returns all the app's blocked account addresses.
func BlockedAddresses() map[string]bool {
	result := make(map[string]bool)
	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range GetMaccPerms() {
			result[addr] = true
		}
	}
	return result
}
