package stoc

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	modulev1 "stoc/api/stoc/stoc"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: modulev1.Query_ServiceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Shows the parameters of the module",
				},
				{
					RpcMethod: "Token",
					Use:       "token [minimal_denom]",
					Short:     "Query a token by its minimal denom (e.g. MYTOKEN_0)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "minimal_denom"},
					},
				},
				{
					RpcMethod: "Tokens",
					Use:       "tokens",
					Short:     "List all tokens with pagination",
				},
				{
					RpcMethod: "TokensBySymbol",
					Use:       "tokens-by-symbol [symbol]",
					Short:     "Query tokens by symbol (e.g. MYTOKEN)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "symbol"},
					},
				},
				{
					RpcMethod: "BalancesWithMetadata",
					Use:       "balances-with-metadata [address]",
					Short:     "Query account balances enriched with token metadata (decimals, symbol, logo)",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "address"},
					},
				},
				// this line is used by ignite scaffolding # autocli/query
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              modulev1.Msg_ServiceDesc.ServiceName,
			EnhanceCustomCommand: true, // only required if you want to use the custom command
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // skipped because authority gated
				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}
