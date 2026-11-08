package app

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingexported "github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
	"github.com/cosmos/cosmos-sdk/x/group"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	"github.com/ethereum/go-ethereum/common"

	stocmodulekeeper "stoc/x/stoc/keeper"
)

// moduleAccountAddrs returns the address of every module account in the
// permission list, keyed by raw address bytes, mapped to the module name.
// The set is fixed at build time, so it is computed once at app init.
func moduleAccountAddrs() map[string]string {
	addrs := make(map[string]string, len(moduleAccPerms))
	for _, p := range moduleAccPerms {
		addrs[string(authtypes.NewModuleAddress(p.Account))] = p.Account
	}
	return addrs
}

// ---------------------------------------------------------------------------
// ICS-20 escrow index (custom-token IBC SendRestriction)
// ---------------------------------------------------------------------------

// indexTransferEscrows records the escrow address of every existing
// transfer-port channel, in any state, in the x/stoc escrow index. Used by
// the v5.0.3 upgrade handler (channels opened before the index existed) and
// by InitChain (channels imported from genesis; the index itself is not part
// of the exported x/stoc genesis). Idempotent.
func (app *App) indexTransferEscrows(ctx sdk.Context) int {
	channels := app.IBCKeeper.ChannelKeeper.GetAllChannelsWithPortPrefix(ctx, ibctransfertypes.PortID)
	n := 0
	for _, ch := range channels {
		if ch.PortId != ibctransfertypes.PortID {
			continue
		}
		app.StocKeeper.SetIBCEscrow(ctx, ibctransfertypes.GetEscrowAddress(ch.PortId, ch.ChannelId), ch.ChannelId)
		n++
	}
	return n
}

// ---------------------------------------------------------------------------
// Transfer channel handshake guard (IBC middleware)
// ---------------------------------------------------------------------------

var (
	_ porttypes.IBCModule             = transferChannelGuard{}
	_ porttypes.PacketDataUnmarshaler = transferChannelGuard{}
)

// transferChannelGuard sits on top of the ICS-20 transfer stack. When a
// transfer channel is created (OnChanOpenInit / OnChanOpenTry) it records the
// channel's escrow address in the x/stoc escrow index, as an ordinary state
// write in the same tx (it reverts if the tx fails). The channel is therefore
// blocked for custom tokens from the next message on, in the same block.
// Ack, Confirm and packet callbacks pass through unchanged.
//
// The callbacks must cost the same whatever the escrow holds. Pre-funded
// escrow (audit F4): an escrow address is deterministic, so anything can be
// sent to the escrow of a channel that does not exist yet. The next channel
// id is global and is only consumed by a successful open, so if the open
// failed because of what the escrow holds (an explicit rejection, or gas
// growing with the number of pre-funded denoms until it hits the block gas
// cap) anyone could block every future transfer channel forever. The guard
// therefore never looks at the escrow's balances: the open always proceeds
// at a flat cost, and pre-funded custom tokens simply stay frozen — once
// indexed, the SendRestriction rejects any custom-token credit into or out of
// that escrow, and ICS-20 unescrow of an untracked amount fails its
// total-escrow accounting.
type transferChannelGuard struct {
	porttypes.IBCModule
	stoc stocmodulekeeper.Keeper
}

func newTransferChannelGuard(app porttypes.IBCModule, stoc stocmodulekeeper.Keeper) transferChannelGuard {
	if app == nil {
		panic("transferChannelGuard: underlying IBC module must not be nil")
	}
	return transferChannelGuard{IBCModule: app, stoc: stoc}
}

// OnChanOpenInit implements porttypes.IBCModule.
func (g transferChannelGuard) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	v, err := g.IBCModule.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, counterparty, version)
	if err != nil {
		return v, err
	}
	g.recordEscrow(ctx, portID, channelID)
	return v, nil
}

// OnChanOpenTry implements porttypes.IBCModule.
func (g transferChannelGuard) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID,
	channelID string,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (string, error) {
	v, err := g.IBCModule.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, counterparty, counterpartyVersion)
	if err != nil {
		return v, err
	}
	g.recordEscrow(ctx, portID, channelID)
	return v, nil
}

// UnmarshalPacketData implements porttypes.PacketDataUnmarshaler by
// delegating to the wrapped stack.
func (g transferChannelGuard) UnmarshalPacketData(ctx sdk.Context, portID, channelID string, bz []byte) (any, string, error) {
	u, ok := g.IBCModule.(porttypes.PacketDataUnmarshaler)
	if !ok {
		return nil, "", fmt.Errorf("transfer stack %T does not implement PacketDataUnmarshaler", g.IBCModule)
	}
	return u.UnmarshalPacketData(ctx, portID, channelID, bz)
}

// recordEscrow indexes the escrow of a new transfer channel: one flat-cost
// state write. It runs only after the wrapped transfer module accepted the
// channel, and that module rejects any port other than its bound port
// (ValidateTransferChannelParams), so portID is the transfer port here.
func (g transferChannelGuard) recordEscrow(ctx sdk.Context, portID, channelID string) {
	g.stoc.SetIBCEscrow(ctx, ibctransfertypes.GetEscrowAddress(portID, channelID), channelID)
}

// ---------------------------------------------------------------------------
// Wallet guard (custom-token non-wallet SendRestriction + tax recipient check)
// ---------------------------------------------------------------------------

// customTokenWalletGuard decides whether an address is a plain user wallet
// that may hold x/stoc custom tokens. Custom tokens may only move wallet <->
// wallet (plus self-burn through the x/stoc module).
type customTokenWalletGuard struct {
	ak          authkeeper.AccountKeeper
	group       groupkeeper.Keeper
	evm         *evmkeeper.Keeper
	stoc        stocmodulekeeper.Keeper
	moduleAddrs map[string]string
	precompiles map[string]bool // bech32 -> true; the app's blockedPrecompileAddrs
}

func newCustomTokenWalletGuard(ak authkeeper.AccountKeeper, gk groupkeeper.Keeper, evm *evmkeeper.Keeper, stoc stocmodulekeeper.Keeper, precompiles map[string]bool) customTokenWalletGuard {
	if evm == nil {
		panic("customTokenWalletGuard: EVM keeper must be initialized before the custom-token restrictions")
	}
	return customTokenWalletGuard{ak: ak, group: gk, evm: evm, stoc: stoc, moduleAddrs: moduleAccountAddrs(), precompiles: precompiles}
}

func nonWalletErr(ctx sdk.Context, denom, direction string, addr sdk.AccAddress, kind string) error {
	ctx.Logger().Warn("Blocked custom token send involving "+kind,
		"denom", denom, "direction", direction, "addr", addr.String(),
	)
	return fmt.Errorf(
		"custom token %q cannot be sent %s %s %s: custom stoc tokens may only move between regular user wallets",
		denom, direction, kind, addr.String(),
	)
}

// rejectIfNonWallet returns an error (and logs) if addr is not a regular
// user wallet, see nonWalletKind. The x/stoc module account is exempted by
// the caller.
func (g customTokenWalletGuard) rejectIfNonWallet(ctx sdk.Context, addr sdk.AccAddress, denom, direction string) error {
	if kind := g.nonWalletKind(ctx, addr); kind != "" {
		return nonWalletErr(ctx, denom, direction, addr, kind)
	}
	return nil
}

// nonWalletKind returns "" when addr is a regular user wallet that may hold
// custom tokens, otherwise a short description of what it is:
//
//   - the address of any module account in the permission list, even when
//     the account does not exist yet (lazily created module accounts)
//   - *icatypes.InterchainAccount
//   - any vestingexported.VestingAccount
//   - any sdk.ModuleAccountI
//   - an account whose public key is an ADR-028 module credential (group
//     policies and other module-derived accounts; nobody holds a key)
//   - an x/group group policy
//   - an EVM contract (non-empty code that is not an EIP-7702 delegation)
//
// A non-existent, non-module address is a fresh wallet. The store reads
// depend only on state, so the gas is deterministic.
func (g customTokenWalletGuard) nonWalletKind(ctx sdk.Context, addr sdk.AccAddress) string {
	if name, ok := g.moduleAddrs[string(addr)]; ok {
		return "module account (" + name + ")"
	}

	acc := g.ak.GetAccount(ctx, addr)
	if acc == nil {
		return ""
	}

	if _, isICA := acc.(*icatypes.InterchainAccount); isICA {
		return "interchain account"
	}
	if _, isVesting := acc.(vestingexported.VestingAccount); isVesting {
		return "vesting account"
	}
	if _, isModule := acc.(sdk.ModuleAccountI); isModule {
		return "module account"
	}

	// A key-controlled account (secp256k1 / eth_secp256k1 / multisig ...) can
	// be neither a group policy nor an EVM contract: both are keyless. Skip
	// the store lookups below for them.
	if pk := acc.GetPubKey(); pk != nil {
		if _, isModuleCred := pk.(*authtypes.ModuleCredential); isModuleCred {
			return "module-derived account"
		}
		return ""
	}

	if g.isGroupPolicy(ctx, addr) {
		return "group policy"
	}
	if g.isEVMContract(ctx, addr) {
		return "EVM contract"
	}
	return ""
}

func (g customTokenWalletGuard) isGroupPolicy(ctx sdk.Context, addr sdk.AccAddress) bool {
	_, err := g.group.GroupPolicyInfo(ctx, &group.QueryGroupPolicyInfoRequest{Address: addr.String()})
	return err == nil
}

// isEVMContract reports whether addr holds EVM code that is not an EIP-7702
// delegation designator. A delegated EOA still has a private key, so it is
// treated as a wallet.
func (g customTokenWalletGuard) isEVMContract(ctx sdk.Context, addr sdk.AccAddress) bool {
	if len(addr) != common.AddressLength {
		return false
	}
	ethAddr := common.BytesToAddress(addr)
	if g.evm.GetCodeHash(ctx, ethAddr) == common.BytesToHash(evmtypes.EmptyCodeHash) {
		return false
	}
	return g.evm.IsContract(ctx, ethAddr)
}

// checkTaxRecipient is installed into the x/stoc keeper and called by
// MsgCreateToken after its own checks (address valid, not bank-blocked,
// account exists, plain *authtypes.BaseAccount, no module credential, not an
// escrow). The v5.0.3 upgrade handler also uses it as a pre-flight on
// existing tokens. On top of nonWalletKind it rejects EVM precompile and
// ICS-20 escrow addresses, which can never receive custom tokens (the send
// path checks those in their own restrictions).
func (g customTokenWalletGuard) checkTaxRecipient(ctx sdk.Context, addr sdk.AccAddress) error {
	if g.precompiles[addr.String()] {
		return fmt.Errorf("%s is an EVM precompile address, not a regular user wallet", addr.String())
	}
	if g.stoc.IsIBCEscrow(ctx, addr) {
		return fmt.Errorf("%s is an IBC transfer escrow address, not a regular user wallet", addr.String())
	}
	if kind := g.nonWalletKind(ctx, addr); kind != "" {
		return fmt.Errorf("%s is a %s, not a regular user wallet", addr.String(), kind)
	}
	return nil
}
