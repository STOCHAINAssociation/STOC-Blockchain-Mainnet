//go:build !stocrelease
// +build !stocrelease

// SA-C9 + SA-C10 audit-2026-05-29: dev-only commands (in-place-testnet,
// multi-node) are registered HERE so production releases built with
// `-tags stocrelease` exclude them entirely. See commands_devcmds_release.go
// for the no-op stub used in release builds.

package cmd

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/types/module"
)

func addDevOnlyCmds(rootCmd *cobra.Command, basicManager module.BasicManager, addStartFlags servertypes.ModuleInitFlags) {
	rootCmd.AddCommand(
		NewInPlaceTestnetCmd(addStartFlags),
		NewTestnetMultiNodeCmd(basicManager, banktypes.GenesisBalancesIterator{}),
	)
}
