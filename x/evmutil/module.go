package evmutil

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"stoc/x/evmutil/keeper"
	"stoc/x/evmutil/types"
)

var (
	_ module.AppModuleBasic = AppModuleBasic{}
	_ module.HasGenesis     = AppModule{}
	_ appmodule.AppModule   = AppModule{}
)

// AppModuleBasic defines the basic application module used by the evmutil module.
type AppModuleBasic struct{}

// Name returns the evmutil module's name.
func (AppModuleBasic) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the evmutil module's types on the given LegacyAmino codec.
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {}

// RegisterInterfaces registers the module's interface types
func (b AppModuleBasic) RegisterInterfaces(_ codectypes.InterfaceRegistry) {}

// DefaultGenesis returns default genesis state as raw bytes for the evmutil module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis performs genesis state validation for the evmutil module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return gs.Validate()
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the evmutil module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {}

// GetTxCmd returns no root tx command for the evmutil module.
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return nil
}

// GetQueryCmd returns no root query command for the evmutil module.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return nil
}

// AppModule implements an application module for the evmutil module.
type AppModule struct {
	AppModuleBasic
	keeper keeper.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(keeper keeper.Keeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{},
		keeper:         keeper,
	}
}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// RegisterInvariants registers the evmutil module invariants.
//
// SA-M4 audit-2026-05-29 (FALSE POSITIVE — by design): evmutil has no
// persistent state of its own. The module's storeKey is declared but never
// written: all "EVM-side" balances (astoc) are computed as a virtual view
// over the underlying Cosmos balances (ustoc) — see EvmBankKeeper.GetSupply
// (astoc supply == ustoc supply × 10^12 by construction). There is no
// boundary accounting that can drift, so any registered invariant would be
// a tautology. Conversion rounding (round-UP for inflow, round-DOWN for
// outflow per SA-C7/C8) is per-operation and never accumulates as state.
// If future work introduces a separate escrow account, register an
// invariant comparing escrow_ustoc × 10^12 to total_astoc supply.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {}

// InitGenesis performs genesis initialization for the evmutil module. It returns no validator updates.
// The evmutil module has no persistent genesis state — it derives EVM denom conversion
// rules from sdk.DefaultBondDenom at runtime. The genesis data is unmarshaled only for validation.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	var gs types.GenesisState
	cdc.MustUnmarshalJSON(data, &gs)
}

// ExportGenesis returns the exported genesis state as raw bytes for the evmutil module.
// The evmutil module has no persistent state — denomination conversion is derived at runtime
// from sdk.DefaultBondDenom, so DefaultGenesisState is always returned.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := types.DefaultGenesisState()
	return cdc.MustMarshalJSON(gs)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }

// BeginBlock returns the begin blocker for the evmutil module.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return nil
}

// EndBlock returns the end blocker for the evmutil module.
func (am AppModule) EndBlock(ctx context.Context) error {
	return nil
}
