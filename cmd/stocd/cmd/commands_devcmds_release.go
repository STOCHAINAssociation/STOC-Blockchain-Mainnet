//go:build stocrelease
// +build stocrelease

// SA-C9 + SA-C10 audit-2026-05-29: release-build stub. Dev commands
// (in-place-testnet, multi-node) are NOT registered in production binaries
// to prevent operator-fat-finger state destruction. Build with `-tags stocrelease`
// to use this stub.

package cmd

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/types/module"
)

func addDevOnlyCmds(_ *cobra.Command, _ module.BasicManager, _ servertypes.ModuleInitFlags) {
	// no-op in release builds
}
