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

	// Enable JSON-RPC by default so EVM is accessible out of the box
	srvCfg.JSONRPC.Enable = true
	srvCfg.JSONRPC.Address = "0.0.0.0:8545"

	// Enable REST API by default
	srvCfg.API.Enable = true

	customAppConfig := CustomAppConfig{
		Config: *srvCfg,
	}

	// SDK base template + EVM/JSON-RPC/TLS template
	customAppTemplate := serverconfig.DefaultConfigTemplate + evmserverconfig.DefaultEVMConfigTemplate

	return customAppTemplate, customAppConfig
}
