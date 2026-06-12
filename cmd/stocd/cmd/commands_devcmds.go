//go:build !stocrelease
// +build !stocrelease

// SA-C9 + SA-C10 audit-2026-05-29: dev-only commands (in-place-testnet,
// multi-node) are registered HERE so production releases built with
// `-tags stocrelease` exclude them entirely. See commands_devcmds_release.go
// for the no-op stub used in release builds.

package cmd

import (
	"fmt"
	"os"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/types/module"
)

// devCmdsEnableEnv gates the dev-only commands at RUNTIME on top of the
// build tag.
//
// SA-AUDIT-2026-06-11 fix19 A16-CROSS-L3: belt-and-braces — if a dev-tagged
// binary ever leaks onto a production host (operator copies the wrong
// artifact), the state-rewriting commands still refuse to run without an
// explicit opt-in. A chain-id check is NOT viable here because devnet runs
// mainnet-parity chain-id "stoc" by design (replay-mainnet workflow).
const devCmdsEnableEnv = "STOCD_ENABLE_DEV_CMDS"

func addDevOnlyCmds(rootCmd *cobra.Command, basicManager module.BasicManager, addStartFlags servertypes.ModuleInitFlags) {
	if os.Getenv(devCmdsEnableEnv) != "1" {
		rootCmd.AddCommand(
			devCmdDisabledStub("in-place-testnet"),
			devCmdDisabledStub("multi-node"),
		)
		return
	}
	rootCmd.AddCommand(
		NewInPlaceTestnetCmd(addStartFlags),
		NewTestnetMultiNodeCmd(basicManager, banktypes.GenesisBalancesIterator{}),
	)
}

// devCmdDisabledStub keeps the command name discoverable (so operators get a
// clear error instead of cobra's "unknown command") while refusing to run.
func devCmdDisabledStub(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("[disabled] dev-only command — set %s=1 to enable", devCmdsEnableEnv),
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%q is a dev-only state-rewriting command; set %s=1 in the environment to enable it (A16-CROSS-L3)", name, devCmdsEnableEnv)
		},
	}
}
