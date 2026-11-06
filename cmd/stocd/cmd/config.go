package cmd

import (
	cmtcfg "github.com/cometbft/cometbft/config"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	evmserverconfig "github.com/cosmos/evm/server/config"
)

// initCometBFTConfig helps to override default CometBFT Config values.
// return cmtcfg.DefaultConfig if no custom configuration is required for the application.
func initCometBFTConfig() *cmtcfg.Config {
	cfg := cmtcfg.DefaultConfig()

	// these values put a higher strain on node memory
	// cfg.P2P.MaxNumInboundPeers = 100
	// cfg.P2P.MaxNumOutboundPeers = 40

	return cfg
}

// initAppConfig helps to override default appConfig template and configs.
// return "", nil if no custom configuration is required for the application.
func initAppConfig() (string, interface{}) {
	type CustomAppConfig struct {
		evmserverconfig.Config `mapstructure:",squash"`
	}

	srvCfg := evmserverconfig.DefaultConfig()

	// SA-C11 audit-2026-05-29: JSON-RPC + REST API default to false (upstream cosmos/evm
	// behavior). Operators wishing to serve EVM dapps must explicitly opt in via app.toml.
	// Previous default-on behavior would auto-open port 8545/1317 on every fresh `stocd init`
	// → Docker port-publish accidents and personal_* namespace abuse have drained
	// other chains. Keep listeners localhost-only when enabled.
	srvCfg.JSONRPC.Address = "127.0.0.1:8545"

	customAppConfig := CustomAppConfig{
		Config: *srvCfg,
	}

	// SDK base template + EVM/JSON-RPC/TLS template
	customAppTemplate := serverconfig.DefaultConfigTemplate + evmserverconfig.DefaultEVMConfigTemplate

	return customAppTemplate, customAppConfig
}
