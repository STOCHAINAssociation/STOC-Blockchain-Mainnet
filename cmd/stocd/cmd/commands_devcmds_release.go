//go:build stocrelease
// +build stocrelease

// Release-build stub. Dev commands (in-place-testnet, multi-node) are not
// registered in release binaries, preventing accidental local state
// destruction. Build with `-tags stocrelease` to use this stub.

package cmd

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/types/module"
)

func addDevOnlyCmds(_ *cobra.Command, _ module.BasicManager, _ servertypes.ModuleInitFlags) {
	// no-op in release builds
}
