package app

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	icacontrollertypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/controller/types"
	icagenesistypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/genesis/types"
	icahosttypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibccoretypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	stoctypes "stoc/x/stoc/types"
)

func runV503Handler(t *testing.T, a *App, ctx sdk.Context) error {
	t.Helper()
	_, err := a.upgradeHandlerV503(ctx, upgradetypes.Plan{Name: UpgradeNameV503, Height: ctx.BlockHeight()}, a.ModuleManager.GetVersionMap())
	return err
}

// mainnetLikeState sets the live mainnet/testnet values measured on
// 2026-11-05: ICA host on with "*", controller on, IBC allowed clients "*".
func mainnetLikeState(a *App, ctx sdk.Context) {
	a.ICAHostKeeper.SetParams(ctx, icahosttypes.NewParams(true, []string{icahosttypes.AllowAllHostMsgs}))
	a.ICAControllerKeeper.SetParams(ctx, icacontrollertypes.NewParams(true))
	a.IBCKeeper.ClientKeeper.SetParams(ctx, ibcclienttypes.DefaultParams())
}

func squat(t *testing.T, a *App, ctx sdk.Context, addr sdk.AccAddress, acc sdk.AccountI, ustoc int64) {
	t.Helper()
	a.AccountKeeper.SetAccount(ctx, acc)
	coins := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, ustoc))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", coins))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, authtypes.NewModuleAddress("mint"), addr, coins))
}

func maccPerms(name string) []string {
	for _, p := range moduleAccPerms {
		if p.Account == name {
			return p.Permissions
		}
	}
	return nil
}

// FIX-1: a BaseAccount (or any non-module account without a key) squatting
// a module address is converted back into the module account; number,
// sequence and balance are kept.
func TestV503Upgrade_SquattedModuleAccountRestored(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)

	transferAddr := authtypes.NewModuleAddress(ibctransfertypes.ModuleName)
	transferSquat := a.AccountKeeper.NewAccountWithAddress(ctx, transferAddr)
	require.NoError(t, transferSquat.SetSequence(3))
	squat(t, a, ctx, transferAddr, transferSquat, 1)

	icaAddr := authtypes.NewModuleAddress(icatypes.ModuleName)
	icaBase := authtypes.NewBaseAccountWithAddress(icaAddr)
	icaBase.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
	icaSquat, err := vestingtypes.NewPermanentLockedAccount(icaBase, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 7)))
	require.NoError(t, err)
	squat(t, a, ctx, icaAddr, icaSquat, 7)

	require.Panics(t, func() { a.AccountKeeper.GetModuleAccount(ctx, ibctransfertypes.ModuleName) },
		"precondition: a squatted module address makes GetModuleAccount panic")

	require.NoError(t, runV503Handler(t, a, ctx))

	for _, tc := range []struct {
		name string
		addr sdk.AccAddress
		prev sdk.AccountI
		bal  int64
	}{
		{ibctransfertypes.ModuleName, transferAddr, transferSquat, 1},
		{icatypes.ModuleName, icaAddr, icaSquat, 7},
	} {
		acc := a.AccountKeeper.GetAccount(ctx, tc.addr)
		macc, ok := acc.(*authtypes.ModuleAccount)
		require.True(t, ok, "%s: got %T", tc.name, acc)
		require.Equal(t, tc.name, macc.Name)
		require.Equal(t, maccPerms(tc.name), macc.Permissions)
		require.Equal(t, tc.prev.GetAccountNumber(), macc.AccountNumber)
		require.Equal(t, tc.prev.GetSequence(), macc.Sequence)
		require.Equal(t, tc.bal, a.BankKeeper.GetBalance(ctx, tc.addr, sdk.DefaultBondDenom).Amount.Int64())
		require.NotPanics(t, func() { a.AccountKeeper.GetModuleAccount(ctx, tc.name) })
	}

	// Every module account of the permission list now exists as such.
	for _, p := range moduleAccPerms {
		_, ok := a.AccountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(p.Account)).(sdk.ModuleAccountI)
		require.True(t, ok, "module account %s must exist after the upgrade", p.Account)
	}
}

// A key can never exist for a module address; if one is found the upgrade
// aborts and writes nothing.
func TestV503Upgrade_SquattedWithPubKeyAborts(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	addr := authtypes.NewModuleAddress(icatypes.ModuleName)
	base := authtypes.NewBaseAccountWithAddress(addr)
	require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
	base.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
	a.AccountKeeper.SetAccount(ctx, base)

	require.ErrorContains(t, runV503Handler(t, a, ctx), "refusing to convert")
	_, isBase := a.AccountKeeper.GetAccount(ctx, addr).(*authtypes.BaseAccount)
	require.True(t, isBase, "nothing may be written when the handler aborts")
	require.Nil(t, a.AccountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(ibctransfertypes.ModuleName)))
}

// FIX-2 + FIX-3: ICA off, IBC clients restricted to 07-tendermint.
func TestV503Upgrade_ICAAndIBCClientParams(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	mainnetLikeState(a, ctx)

	require.NoError(t, runV503Handler(t, a, ctx))

	host := a.ICAHostKeeper.GetParams(ctx)
	require.False(t, host.HostEnabled)
	require.Empty(t, host.AllowMessages)
	require.False(t, a.ICAControllerKeeper.GetParams(ctx).ControllerEnabled)

	clientParams := a.IBCKeeper.ClientKeeper.GetParams(ctx)
	require.Equal(t, []string{ibcexported.Tendermint}, clientParams.AllowedClients)
	require.False(t, clientParams.IsAllowedClient(ibcexported.Solomachine))
	require.False(t, clientParams.IsAllowedClient(ibcexported.Localhost))
}

// FIX-4: missing tax recipients of taxed tokens are created as plain
// BaseAccounts; existing recipients (mainnet ASL_8 shape) are untouched.
func TestV503Upgrade_TaxRecipients(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	creator := sdk.AccAddress([]byte("creator_____________"))

	token := func(denom string, pct math.LegacyDec, recipient sdk.AccAddress) {
		tk := v503Token(creator, pct, recipient.String())
		tk.Id, tk.MinimalDenom, tk.Symbol = denom, denom, denom[:3]
		require.NoError(t, a.StocKeeper.SetToken(ctx, tk))
	}

	missing := sdk.AccAddress([]byte("tax_missing_________"))
	token("MIS_1", math.LegacyNewDecWithPrec(1, 1), missing)

	existing := sdk.AccAddress([]byte("tax_existing________")) // ASL_8: 0.1% tax, BaseAccount recipient
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, existing))
	existingNum := a.AccountKeeper.GetAccount(ctx, existing).GetAccountNumber()
	token("ASL_8", math.LegacyNewDecWithPrec(1, 3), existing)

	locked := sdk.AccAddress([]byte("tax_locked__________"))
	lockedBase := authtypes.NewBaseAccountWithAddress(locked)
	lockedBase.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
	lockedAcc, err := vestingtypes.NewPermanentLockedAccount(lockedBase, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))
	require.NoError(t, err)
	a.AccountKeeper.SetAccount(ctx, lockedAcc)
	token("LCK_2", math.LegacyNewDecWithPrec(1, 1), locked)

	untaxed := sdk.AccAddress([]byte("tax_untaxed_________"))
	token("NOT_3", math.LegacyZeroDec(), untaxed)

	require.NoError(t, runV503Handler(t, a, ctx))

	created, ok := a.AccountKeeper.GetAccount(ctx, missing).(*authtypes.BaseAccount)
	require.True(t, ok, "missing tax recipient must be created as a BaseAccount")
	require.Nil(t, created.GetPubKey())

	require.Equal(t, existingNum, a.AccountKeeper.GetAccount(ctx, existing).GetAccountNumber())
	_, stillLocked := a.AccountKeeper.GetAccount(ctx, locked).(*vestingtypes.PermanentLockedAccount)
	require.True(t, stillLocked, "non-wallet recipients are logged and left as is")
	require.Nil(t, a.AccountKeeper.GetAccount(ctx, untaxed), "untaxed tokens are ignored")

	// The materialized recipient can no longer be turned into a vesting account.
	require.True(t, a.AccountKeeper.HasAccount(ctx, missing))

	// Only the non-wallet recipient is reported.
	reported := notWalletEvents(ctx)
	require.Equal(t, map[string]string{"LCK_2": locked.String()}, reported)
}

// notWalletEvents returns denom -> recipient of every
// upgrade_v503_tax_recipient_not_wallet event emitted on ctx.
func notWalletEvents(ctx sdk.Context) map[string]string {
	out := map[string]string{}
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type != "upgrade_v503_tax_recipient_not_wallet" {
			continue
		}
		var denom, recipient string
		for _, attr := range ev.Attributes {
			switch attr.Key {
			case "denom":
				denom = attr.Value
			case "recipient":
				recipient = attr.Value
			}
		}
		out[denom] = recipient
	}
	return out
}

// Audit round 2 pre-flight: a tax recipient that looks like a plain
// BaseAccount but is an EVM contract or a group policy cannot receive custom
// tokens, so every taxed transfer of the token fails after the upgrade. The
// handler reports it (event + Error log) without aborting.
func TestV503Upgrade_TaxRecipientPreflight(t *testing.T) {
	for _, kind := range []string{"evm-contract", "group-policy", "keyed-wallet"} {
		t.Run(kind, func(t *testing.T) {
			a := v503App(t)
			ctx := v503Finalize(a, 500).WithBlockTime(time.Unix(1_700_000_000, 0))
			creator := sdk.AccAddress([]byte("creator_____________"))

			var taxRcpt sdk.AccAddress
			switch kind {
			case "evm-contract":
				taxRcpt = sdk.AccAddress(common.HexToAddress("0x5afe00000000000000000000000000000000cafe").Bytes())
				a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, taxRcpt))
				code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
				hash := crypto.Keccak256Hash(code)
				a.EVMKeeper.SetCode(ctx, hash.Bytes(), code)
				a.EVMKeeper.SetCodeHash(ctx, taxRcpt.Bytes(), hash.Bytes())
			case "group-policy":
				msg, err := group.NewMsgCreateGroupWithPolicy(creator.String(),
					[]group.MemberRequest{{Address: creator.String(), Weight: "1"}}, "", "", false,
					group.NewThresholdDecisionPolicy("1", time.Hour, 0))
				require.NoError(t, err)
				res, err := a.GroupKeeper.CreateGroupWithPolicy(ctx, msg)
				require.NoError(t, err)
				taxRcpt = sdk.MustAccAddressFromBech32(res.GroupPolicyAddress)
			case "keyed-wallet": // mainnet ASL_8 shape
				taxRcpt = sdk.AccAddress([]byte("asl8_tax_recipient__"))
				base := authtypes.NewBaseAccountWithAddress(taxRcpt)
				require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
				base.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
				a.AccountKeeper.SetAccount(ctx, base)
			}
			_, isBase := a.AccountKeeper.GetAccount(ctx, taxRcpt).(*authtypes.BaseAccount)
			require.True(t, isBase, "precondition: looks like a plain BaseAccount")

			tk := v503Token(creator, math.LegacyNewDecWithPrec(1, 3), taxRcpt.String())
			tk.Id, tk.MinimalDenom, tk.Symbol = "ASL_8", "ASL_8", "ASL"
			require.NoError(t, a.StocKeeper.SetToken(ctx, tk))
			c := sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1_000_000))
			require.NoError(t, a.BankKeeper.MintCoins(ctx, stoctypes.ModuleName, c))
			require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, stoctypes.ModuleName, creator, c))

			require.NoError(t, runV503Handler(t, a, ctx), "the pre-flight never aborts the upgrade")

			fresh := sdk.AccAddress([]byte("fresh_recipient_____"))
			err := v503RunTx(t, a, ctx, banktypes.NewMsgSend(creator, fresh, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1000))))
			if kind == "keyed-wallet" {
				require.Empty(t, notWalletEvents(ctx))
				require.NoError(t, err)
				require.Equal(t, int64(999), a.BankKeeper.GetBalance(ctx, fresh, "ASL_8").Amount.Int64())
				require.Equal(t, int64(1), a.BankKeeper.GetBalance(ctx, taxRcpt, "ASL_8").Amount.Int64())
				return
			}
			require.Equal(t, map[string]string{"ASL_8": taxRcpt.String()}, notWalletEvents(ctx))
			require.ErrorContains(t, err, "tax enforcement failed", "taxed transfers of this token fail; the event is the operator signal")
		})
	}
}

// FIX-7: a chain whose EVM params still activate precompiles (old devnets
// started from the former DefaultGenesis) is reset to [].
func TestV503Upgrade_ResetsActivePrecompiles(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)

	p := evmtypes.DefaultParams()
	p.ActiveStaticPrecompiles = []string{"0x0000000000000000000000000000000000000400"}
	bz, err := a.AppCodec().Marshal(&p)
	require.NoError(t, err)
	ctx.KVStore(a.GetKey(evmtypes.StoreKey)).Set(evmtypes.KeyPrefixParams, bz) // bypasses Validate on purpose
	require.NotEmpty(t, a.EVMKeeper.GetParams(ctx).ActiveStaticPrecompiles)

	require.NoError(t, runV503Handler(t, a, ctx))
	require.Empty(t, a.EVMKeeper.GetParams(ctx).ActiveStaticPrecompiles)
}

// The handler indexes the escrow of every existing transfer-port channel, in
// any state (channels opened before v5.0.3 never went through the guard).
func TestV503Upgrade_IndexesExistingTransferChannels(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	v503SetTransferChannel(a, ctx, "channel-0", channeltypes.OPEN)
	v503SetTransferChannel(a, ctx, "channel-1", channeltypes.INIT)
	v503SetTransferChannel(a, ctx, "channel-2", channeltypes.CLOSED)
	icaPort := icatypes.ControllerPortPrefix + "owner"
	a.IBCKeeper.ChannelKeeper.SetChannel(ctx, icaPort, "channel-3", channeltypes.NewChannel(channeltypes.OPEN, channeltypes.ORDERED,
		channeltypes.NewCounterparty(icatypes.HostPortID, "channel-9"), []string{"connection-0"}, "ics27-1"))

	for run := 0; run < 2; run++ { // idempotent
		require.NoError(t, runV503Handler(t, a, ctx))
		for _, id := range []string{"channel-0", "channel-1", "channel-2"} {
			ch, ok := a.StocKeeper.GetIBCEscrowChannel(ctx, ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, id))
			require.True(t, ok, id)
			require.Equal(t, id, ch)
		}
		require.False(t, a.StocKeeper.IsIBCEscrow(ctx, ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, "channel-3")))
		require.False(t, a.StocKeeper.IsIBCEscrow(ctx, ibctransfertypes.GetEscrowAddress(icaPort, "channel-3")))
	}
}

// x/erc20 registration becomes governance-only; EnableErc20 is kept as is.
// Idempotent.
func TestV503Upgrade_Erc20PermissionlessRegistrationOff(t *testing.T) {
	for _, enable := range []bool{true, false} {
		a := v503App(t)
		ctx := v503Finalize(a, 100)
		require.NoError(t, a.Erc20Keeper.SetParams(ctx, erc20types.NewParams(enable, true)))

		for run := 0; run < 2; run++ {
			require.NoError(t, runV503Handler(t, a, ctx))
			require.Equal(t, erc20types.NewParams(enable, false), a.Erc20Keeper.GetParams(ctx), "enable_erc20=%v run %d", enable, run)
		}
	}
}

// After the upgrade a non-gov MsgRegisterERC20 fails the authority check;
// the gov authority passes it. The probes stop before any EVM call: the
// routed message carries a valid but unregistered address and fails at the
// authority check, and the direct keeper call carries an invalid address so
// a signer that passes the authority check fails at the address loop. The
// full gov registration of a deployed ERC-20 is covered in app/e2etest.
func TestV503Upgrade_Erc20RegistrationGovernanceOnly(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	require.NoError(t, a.Erc20Keeper.SetParams(ctx, erc20types.NewParams(true, true))) // mainnet today

	user := sdk.AccAddress([]byte("user________________")).String()
	gov := authtypes.NewModuleAddress("gov").String()
	// keeperGate calls the erc20 msg server method directly (no
	// ValidateBasic) with an invalid address: an "invalid ERC20 contract
	// address" error means the signer passed the authority check.
	keeperGate := func(signer string) error {
		branch, _ := ctx.CacheContext()
		_, err := a.Erc20Keeper.RegisterERC20(branch, &erc20types.MsgRegisterERC20{Signer: signer, Erc20Addresses: []string{"not-a-hex-address"}})
		return err
	}

	require.ErrorContains(t, keeperGate(user), "invalid ERC20 contract address", "before: anyone passes the authority check")

	require.NoError(t, runV503Handler(t, a, ctx))

	// Through the msg router (ValidateBasic + msg server), as a tx would.
	msg := &erc20types.MsgRegisterERC20{Signer: user, Erc20Addresses: []string{"0x00000000000000000000000000000000000c0de1"}}
	h := a.MsgServiceRouter().Handler(msg)
	require.NotNil(t, h)
	branch, _ := ctx.CacheContext()
	_, err := h(branch, msg)
	require.ErrorIs(t, err, govtypes.ErrInvalidSigner)
	require.ErrorContains(t, err, "invalid authority")

	require.ErrorIs(t, keeperGate(user), govtypes.ErrInvalidSigner)
	require.ErrorContains(t, keeperGate(gov), "invalid ERC20 contract address", "after: gov still passes the authority check")
}

// Running the handler twice changes nothing the second time.
func TestV503Upgrade_Idempotent(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	mainnetLikeState(a, ctx)
	transferAddr := authtypes.NewModuleAddress(ibctransfertypes.ModuleName)
	squat(t, a, ctx, transferAddr, a.AccountKeeper.NewAccountWithAddress(ctx, transferAddr), 1)
	creator := sdk.AccAddress([]byte("creator_____________"))
	missing := sdk.AccAddress([]byte("tax_missing_________"))
	require.NoError(t, a.StocKeeper.SetToken(ctx, v503Token(creator, math.LegacyNewDecWithPrec(1, 1), missing.String())))

	require.NoError(t, runV503Handler(t, a, ctx))

	type snapshot struct {
		nextAccNum uint64
		accounts   []string
		host       icahosttypes.Params
		controller icacontrollertypes.Params
		clients    ibcclienttypes.Params
		evm        evmtypes.Params
	}
	take := func() snapshot {
		s := snapshot{
			nextAccNum: a.AccountKeeper.NextAccountNumber(ctx),
			host:       a.ICAHostKeeper.GetParams(ctx),
			controller: a.ICAControllerKeeper.GetParams(ctx),
			clients:    a.IBCKeeper.ClientKeeper.GetParams(ctx),
			evm:        a.EVMKeeper.GetParams(ctx),
		}
		a.AccountKeeper.IterateAccounts(ctx, func(acc sdk.AccountI) bool {
			s.accounts = append(s.accounts, acc.String())
			return false
		})
		return s
	}
	// NextAccountNumber itself increments the counter; take two snapshots
	// around the second handler run with the same number of probes.
	first := take()
	require.NoError(t, runV503Handler(t, a, ctx))
	second := take()
	require.Equal(t, first.accounts, second.accounts)
	require.Equal(t, first.host, second.host)
	require.Equal(t, first.controller, second.controller)
	require.Equal(t, first.clients, second.clients)
	require.Equal(t, first.evm, second.evm)
	require.Equal(t, first.nextAccNum+1, second.nextAccNum, "the second run must not allocate account numbers")
}

// DefaultGenesis for new networks equals the post-v5.0.3 mainnet settings.
func TestV503_DefaultGenesis(t *testing.T) {
	a := v503App(t)
	g := a.DefaultGenesis()

	var evmGS evmtypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(g[evmtypes.ModuleName], &evmGS)
	require.Empty(t, evmGS.Params.ActiveStaticPrecompiles)
	require.NoError(t, evmGS.Params.Validate())

	var erc20GS erc20types.GenesisState
	a.AppCodec().MustUnmarshalJSON(g[erc20types.ModuleName], &erc20GS)
	require.Equal(t, erc20types.NewParams(true, false), erc20GS.Params, "erc20: enabled, governance-only registration")
	require.NoError(t, erc20GS.Validate())

	var icaGS icagenesistypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(g[icatypes.ModuleName], &icaGS)
	require.False(t, icaGS.HostGenesisState.Params.HostEnabled)
	require.Empty(t, icaGS.HostGenesisState.Params.AllowMessages)
	require.False(t, icaGS.ControllerGenesisState.Params.ControllerEnabled)

	var ibcGS ibccoretypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(g[ibcexported.ModuleName], &ibcGS)
	require.Equal(t, []string{ibcexported.Tendermint}, ibcGS.ClientGenesis.Params.AllowedClients)
	require.NoError(t, ibcGS.Validate())
}

// FIX-7: a gov MsgUpdateParams that enables any static precompile fails at
// execution.
func TestV503_EVMUpdateParamsCannotEnablePrecompile(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	gov := authtypes.NewModuleAddress("gov").String()

	p := evmtypes.DefaultParams()
	_, err := a.EVMKeeper.UpdateParams(ctx, &evmtypes.MsgUpdateParams{Authority: gov, Params: p})
	require.NoError(t, err, "control: params with no precompile are accepted")

	for _, pc := range evmtypes.AvailableStaticPrecompiles {
		p.ActiveStaticPrecompiles = []string{pc}
		_, err = a.EVMKeeper.UpdateParams(ctx, &evmtypes.MsgUpdateParams{Authority: gov, Params: p})
		require.ErrorContains(t, err, "active static precompiles are disabled on STOChain", pc)
	}
	require.Empty(t, a.EVMKeeper.GetParams(ctx).ActiveStaticPrecompiles)
}
