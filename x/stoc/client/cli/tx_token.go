package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	cosmossdk_io_math "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"

	"stoc/x/stoc/types"
)

func CmdCreateToken() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-token [symbol] [name] [initial-supply]",
		Short: "Create a new token",
		Args:  cobra.ExactArgs(3),  // Đảm bảo chấp nhận đúng 3 tham số
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			symbol := args[0]
			name := args[1]
			initialSupply := args[2]
			
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Đọc các tham số tùy chọn
			totalSupply, _ := cmd.Flags().GetString("total-supply")
			if totalSupply == "" {
				totalSupply = initialSupply
			}
			
			decimals, _ := cmd.Flags().GetUint64("decimals")
			logo, _ := cmd.Flags().GetString("logo")
			taxStr, _ := cmd.Flags().GetString("tax")
			distributionsStr, _ := cmd.Flags().GetString("distributions")
			
			// Xử lý tax
			var tax types.TokenTax
			if taxStr != "" {
				if err := json.Unmarshal([]byte(taxStr), &tax); err != nil {
					return fmt.Errorf("failed to parse tax JSON: %w", err)
				}
			}
			
			// Xử lý distributions
			var distributions []types.WalletDistribution
			if distributionsStr != "" {
				// Thử wrapping trong một object
				wrappedJSON := fmt.Sprintf(`{"distributions":%s}`, distributionsStr)
				var wrapper struct {
					Distributions []types.WalletDistribution `json:"distributions"`
				}
				if err := json.Unmarshal([]byte(wrappedJSON), &wrapper); err != nil {
					return fmt.Errorf("failed to parse distributions JSON: %w", err)
				}
				distributions = wrapper.Distributions
			}

			// Convert string to cosmossdk.io/math.Int
			initialSupplyInt, ok := cosmossdk_io_math.NewIntFromString(initialSupply)
			if !ok {
				return fmt.Errorf("invalid initial supply: %s", initialSupply)
			}

			totalSupplyInt, ok := cosmossdk_io_math.NewIntFromString(totalSupply)
			if !ok {
				return fmt.Errorf("invalid total supply: %s", totalSupply)
			}
			
			msg := &types.MsgCreateToken{
				Creator:       clientCtx.GetFromAddress().String(),
				Symbol:        symbol,
				Name:          name,
				InitialSupply: initialSupplyInt,
				TotalSupply:   totalSupplyInt,
				Decimals:      uint32(decimals),
				Logo:          logo,
				Tax:           tax,
				Distributions: distributions,
			}
			
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	
	// Thêm các flags
	cmd.Flags().String("total-supply", "", "Total supply (defaults to initial supply)")
	cmd.Flags().Uint64("decimals", 6, "Number of decimal places")
	cmd.Flags().String("logo", "", "URL to token logo")
	cmd.Flags().String("tax", "", "Tax configuration in JSON format")
	cmd.Flags().String("distributions", "", "Token distributions in JSON format")
	
	flags.AddTxFlagsToCmd(cmd)
	
	return cmd
} 