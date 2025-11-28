# STOC EVM Implementation Steps

## Files Created ✅
1. ✅ `app/evm.go` - EVM module setup
2. ✅ `app/ante/ante.go` - Ante handler router
3. ✅ `app/ante/evm_handler.go` - EVM ante handler
4. ✅ `app/upgrades.go` - Upgrade handler for migration
5. ✅ `go.mod` - Updated dependencies

## Files Need Manual Updates

### 1. app/app.go - Add EVM Keepers

#### Step 1.1: Add imports at the top
```go
// Add these imports after existing imports
import (
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
	erc20keeper "github.com/cosmos/evm/x/erc20/keeper"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	"github.com/ethereum/go-ethereum/common"

	// Update IBC imports from v8 to v10
	_ "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts" // CHANGE v8 to v10
	icacontrollerkeeper "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/controller/keeper"
	icahostkeeper "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/host/keeper"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	_ "github.com/cosmos/ibc-go/v10/modules/apps/29-fee" // CHANGE v8 to v10
	ibcfeekeeper "github.com/cosmos/ibc-go/v10/modules/apps/29-fee/keeper"
	ibctransferkeeper "github.com/cosmos/ibc-go/v10/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	// Import new ante package
	stocappante "stoc/app/ante"
)
```

#### Step 1.2: Remove capability module imports
```go
// REMOVE these lines:
_ "github.com/cosmos/ibc-go/modules/capability" // import for side-effects
capabilitykeeper "github.com/cosmos/ibc-go/modules/capability/keeper"
capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"
```

#### Step 1.3: Add EVM keepers to App struct (around line 118)
```go
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
	IBCKeeper           *ibckeeper.Keeper
	// REMOVE CapabilityKeeper and Scoped keepers related to capability
	// CapabilityKeeper    *capabilitykeeper.Keeper
	IBCFeeKeeper        ibcfeekeeper.Keeper
	ICAControllerKeeper icacontrollerkeeper.Keeper
	ICAHostKeeper       icahostkeeper.Keeper
	TransferKeeper      ibctransferkeeper.Keeper

	// Scoped IBC - REMOVE capability related ones
	// ScopedIBCKeeper           capabilitykeeper.ScopedKeeper
	// ScopedIBCTransferKeeper   capabilitykeeper.ScopedKeeper
	// ScopedICAControllerKeeper capabilitykeeper.ScopedKeeper
	// ScopedICAHostKeeper       capabilitykeeper.ScopedKeeper
	// ScopedKeepers             map[string]capabilitykeeper.ScopedKeeper

	// ADD EVM Keepers here:
	EVMKeeper         evmkeeper.Keeper
	FeeMarketKeeper   feemarketkeeper.Keeper
	Erc20Keeper       erc20keeper.Keeper

	// EVM mempool and client context
	EVMMempool         sdkmempool.ExtMempool
	clientCtx          client.Context
	pendingTxListeners []func(common.Hash)

	StocKeeper stocmodulekeeper.Keeper

	sm *module.SimulationManager
}
```

#### Step 1.4: Update New() function - After registerIBCModules (around line 326)
```go
// register legacy modules
if err := app.registerIBCModules(appOpts); err != nil {
	return nil, err
}

// ADD: Register EVM modules
if err := app.registerEVMModules(appOpts); err != nil {
	return nil, err
}

// ADD: Post-register EVM modules (precompiles)
if err := app.postRegisterEVMModules(); err != nil {
	return nil, err
}

// register streaming services
if err := app.RegisterStreamingServices(appOpts, app.kvStoreKeys()); err != nil {
	return nil, err
}
```

#### Step 1.5: Replace NewAnteHandler function (around line 207)
```go
// REPLACE the entire NewAnteHandler function with this:
func NewAnteHandler(
	options stocappante.HandlerOptions,
) (sdk.AnteHandler, error) {
	return stocappante.NewAnteHandler(options)
}
```

#### Step 1.6: Update anteHandler setup (around line 357-371)
```go
// REPLACE anteOptions creation and anteHandler setup:
maxGasWanted := cast.ToUint64(appOpts.Get(server.FlagGasWanted))

anteOptions := stocappante.HandlerOptions{
	HandlerOptions: ante.HandlerOptions{
		AccountKeeper:   app.AccountKeeper,
		BankKeeper:      app.BankKeeper,
		SignModeHandler: app.txConfig.SignModeHandler(),
		FeegrantKeeper:  app.FeeGrantKeeper,
		SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
	},
	IBCKeeper:         app.IBCKeeper,
	EvmKeeper:         &app.EVMKeeper,
	FeeMarketKeeper:   &app.FeeMarketKeeper,
	MaxTxGasWanted:    maxGasWanted,
	PendingTxListener: app.RegisterPendingTxListener,
}

anteHandler, err := NewAnteHandler(anteOptions)
if err != nil {
	return nil, err
}
app.SetAnteHandler(anteHandler)
```

#### Step 1.7: Setup EVM mempool (after SetAnteHandler, around line 371)
```go
app.SetAnteHandler(anteHandler)

// ADD: Setup EVM mempool
app.setEVMMempool()

postHandler := stocante.NewTaxPostDecorator(app.StocKeeper, app.appCodec)
app.SetPostHandler(sdk.ChainPostDecorators(postHandler))
```

#### Step 1.8: Register upgrade handler (before app.Load, around line 376)
```go
postHandler := stocante.NewTaxPostDecorator(app.StocKeeper, app.appCodec)
app.SetPostHandler(sdk.ChainPostDecorators(postHandler))

// ADD: Register upgrade handlers for EVM migration
app.RegisterUpgradeHandlers()

if err := app.Load(loadLatest); err != nil {
	return nil, err
}
```

#### Step 1.9: Remove GetCapabilityScopedKeeper function
```go
// REMOVE this entire function if it exists:
// func (app *App) GetCapabilityScopedKeeper(moduleName string) capabilitykeeper.ScopedKeeper {
//     ...
// }
```

---

### 2. app/app_config.go - Update Module Configuration

#### Step 2.1: Update imports
```go
// Change IBC imports from v8 to v10
icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
ibcfeetypes "github.com/cosmos/ibc-go/v10/modules/apps/29-fee/types"
ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"

// REMOVE capability imports:
// capabilitytypes "github.com/cosmos/ibc-go/modules/capability/types"

// ADD EVM imports:
erc20types "github.com/cosmos/evm/x/erc20/types"
feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
evmtypes "github.com/cosmos/evm/x/vm/types"
```

#### Step 2.2: Update moduleAccPerms (add EVM module accounts)
```go
moduleAccPerms = []*authmodulev1.ModuleAccountPermission{
	{Account: authtypes.FeeCollectorName},
	{Account: distrtypes.ModuleName},
	{Account: minttypes.ModuleName, Permissions: []string{authtypes.Minter}},
	{Account: stakingtypes.BondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
	{Account: stakingtypes.NotBondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
	{Account: govtypes.ModuleName, Permissions: []string{authtypes.Burner}},
	{Account: nft.ModuleName},
	{Account: ibctransfertypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
	{Account: ibcfeetypes.ModuleName},
	{Account: icatypes.ModuleName},
	// ADD EVM module accounts:
	{Account: evmtypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
	{Account: erc20types.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
	{Account: feemarkettypes.ModuleName},
	// this line is used by starport scaffolding # stargate/app/maccPerms
	{Account: stocmoduletypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
}
```

#### Step 2.3: Update genesisModuleOrder
```go
genesisModuleOrder = []string{
	// REMOVE: capabilitytypes.ModuleName,
	authtypes.ModuleName,
	banktypes.ModuleName,
	distrtypes.ModuleName,
	stakingtypes.ModuleName,
	slashingtypes.ModuleName,
	govtypes.ModuleName,
	minttypes.ModuleName,
	crisistypes.ModuleName,
	ibcexported.ModuleName,
	genutiltypes.ModuleName,
	evidencetypes.ModuleName,
	authz.ModuleName,
	ibctransfertypes.ModuleName,
	icatypes.ModuleName,
	ibcfeetypes.ModuleName,
	feegrant.ModuleName,
	paramstypes.ModuleName,
	upgradetypes.ModuleName,
	vestingtypes.ModuleName,
	nft.ModuleName,
	group.ModuleName,
	consensustypes.ModuleName,
	circuittypes.ModuleName,
	// chain modules
	stocmoduletypes.ModuleName,
	// ADD EVM modules BEFORE genutiltypes:
	erc20types.ModuleName,
	feemarkettypes.ModuleName,
	evmtypes.ModuleName,
	// this line is used by starport scaffolding # stargate/app/initGenesis
}
```

#### Step 2.4: Update beginBlockers
```go
beginBlockers = []string{
	minttypes.ModuleName,
	distrtypes.ModuleName,
	slashingtypes.ModuleName,
	evidencetypes.ModuleName,
	stakingtypes.ModuleName,
	authz.ModuleName,
	genutiltypes.ModuleName,
	// REMOVE: capabilitytypes.ModuleName,
	ibcexported.ModuleName,
	ibctransfertypes.ModuleName,
	icatypes.ModuleName,
	ibcfeetypes.ModuleName,
	// ADD EVM modules:
	erc20types.ModuleName,
	feemarkettypes.ModuleName,
	evmtypes.ModuleName,
	// chain modules
	stocmoduletypes.ModuleName,
	// this line is used by starport scaffolding # stargate/app/beginBlockers
}
```

#### Step 2.5: Update endBlockers
```go
endBlockers = []string{
	crisistypes.ModuleName,
	govtypes.ModuleName,
	stakingtypes.ModuleName,
	feegrant.ModuleName,
	group.ModuleName,
	genutiltypes.ModuleName,
	ibcexported.ModuleName,
	ibctransfertypes.ModuleName,
	// REMOVE: capabilitytypes.ModuleName,
	icatypes.ModuleName,
	ibcfeetypes.ModuleName,
	// ADD EVM modules:
	erc20types.ModuleName,
	feemarkettypes.ModuleName,
	evmtypes.ModuleName,
	// chain modules
	stocmoduletypes.ModuleName,
	// this line is used by starport scaffolding # stargate/app/endBlockers
}
```

#### Step 2.6: Add getBlockAccAddrs function (at the end of file)
```go
func getBlockAccAddrs() []string {
	// Add EVM precompile addresses to blocked accounts
	for _, precompile := range evmtypes.AvailableStaticPrecompiles {
		blockAccAddrs = append(blockAccAddrs, precompile)
	}
	return blockAccAddrs
}
```

#### Step 2.7: Update bank module config to use getBlockAccAddrs
```go
{
	Name: banktypes.ModuleName,
	Config: appconfig.WrapAny(&bankmodulev1.Module{
		BlockedModuleAccountsOverride: getBlockAccAddrs(), // CHANGE from blockAccAddrs
	}),
},
```

---

### 3. cmd/stocxxxd/cmd/root.go - Add EVM Commands

#### Step 3.1: Add imports
```go
import (
	// ... existing imports ...

	evmconfig "github.com/cosmos/evm/config"
	evmcli "github.com/cosmos/evm/cmd"
	"stoc/app"
)
```

#### Step 3.2: Register EVM modules in initRootCmd
Find the `initRootCmd` function and add after module manager setup:
```go
func initRootCmd(
	rootCmd *cobra.Command,
	txConfig client.TxConfig,
	interfaceRegistry codectypes.InterfaceRegistry,
	appCodec codec.Codec,
	basicManager module.BasicManager,
) {
	// ... existing code ...

	// ADD: Register EVM modules for client
	evmModules := app.RegisterEVM(appCodec, interfaceRegistry)
	for name, mod := range evmModules {
		basicManager[name] = mod
	}

	// ... rest of function ...
}
```

#### Step 3.3: Add EVM commands
After `rootCmd.AddCommand(...)`, add:
```go
// ADD EVM commands
evmcli.AddCommands(rootCmd, app.RegisterEVM)
```

---

### 4. app/ibc.go - Update IBC Module Registration

Create new file `app/ibc.go` if it doesn't exist, or update existing one:

```go
package app

import (
	"fmt"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	ibctransferkeeper "github.com/cosmos/ibc-go/v10/modules/apps/transfer/keeper"
)

// registerIBCModules register IBC keepers and non dependency inject modules.
func (app *App) registerIBCModules(appOpts servertypes.AppOptions) error {
	// IBC v10 doesn't use capability module anymore
	// Just setup transfer keeper if needed

	// Setup IBC Transfer Keeper
	app.TransferKeeper = ibctransferkeeper.NewKeeper(
		app.appCodec,
		app.GetKey("transfer"),
		app.GetSubspace("transfer"),
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.PortKeeper,
		app.AccountKeeper,
		app.BankKeeper,
		app.ScopeTransferKeeper,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	)

	return nil
}
```

**Note:** Bạn có thể cần adjust based on existing IBC setup.

---

## Migration Plan for Running Chain

### Step A: Prepare Upgrade Proposal

```bash
# Create upgrade proposal (example)
stocxxxd tx gov submit-proposal software-upgrade v2-evm \
  --title="Add EVM Support" \
  --description="This upgrade adds EVM support to STOC chain with 6 decimals" \
  --upgrade-height=<FUTURE_HEIGHT> \
  --upgrade-info='{"binaries":{"linux/amd64":"https://..."}}' \
  --deposit=10000000ustoc \
  --from=<YOUR_KEY> \
  --chain-id=<CHAIN_ID>
```

### Step B: Binary Upgrade Process

1. **Build new binary with EVM**
```bash
make install
# or
make build
```

2. **Before upgrade height:**
   - All validators need the new binary
   - Binary should be placed in correct location
   - Can use Cosmovisor for automatic upgrade

3. **At upgrade height:**
   - Chain will automatically halt
   - Upgrade handler will:
     - Add EVM, FeeMarket, ERC20 store keys
     - Initialize EVM modules with default genesis
     - Migrate state if needed

4. **After upgrade:**
   - Chain resumes with EVM enabled
   - No balance changes (6 decimals preserved)
   - JSON-RPC available at port 8545/8546

### Step C: Verify Migration

```bash
# Check upgrade status
stocxxxd q upgrade plan

# After upgrade, test EVM
curl -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://localhost:8545
```

---

## Summary Checklist

- [ ] `go.mod` updated with EVM dependencies ✅
- [ ] `app/evm.go` created ✅
- [ ] `app/ante/ante.go` created ✅
- [ ] `app/ante/evm_handler.go` created ✅
- [ ] `app/upgrades.go` created ✅
- [ ] `app/app.go` updated (manual)
- [ ] `app/app_config.go` updated (manual)
- [ ] `app/ibc.go` created/updated (manual)
- [ ] `cmd/stocxxxd/cmd/root.go` updated (manual)
- [ ] Run `go mod tidy`
- [ ] Test build: `make build`
- [ ] Test locally
- [ ] Create upgrade proposal
- [ ] Coordinate with validators
- [ ] Execute upgrade

---

## Important Notes

1. **Decimals = 6**: EVM configured with 6 decimals in `app/evm.go:58`
2. **No state reset needed**: Upgrade handler adds stores without resetting
3. **IBC v10**: Capability module removed, imports updated
4. **Backup**: Always backup before upgrade!
5. **Test on testnet first**: Critical!

---

## Next Steps

1. Complete manual file updates above
2. Run `go mod tidy` in stoc directory
3. Test build
4. Test on local node
5. Deploy to testnet with upgrade
6. Monitor and adjust
7. Deploy to mainnet

Good luck! 🚀
