package types

// Parameter store key
var (
	ParamStoreKeyEnableErc20                = []byte("EnableErc20") // figure out where this is initialized
	ParamStoreKeyPermissionlessRegistration = []byte("PermissionlessRegistration")
)

var (
	CtxKeyDynamicPrecompiles = "DynamicPrecompiles"
	CtxKeyNativePrecompiles  = "NativePrecompiles"
)

func NewParams(
	enableErc20 bool,
	permissionlessRegistration bool,
) Params {
	return Params{
		EnableErc20:                enableErc20,
		PermissionlessRegistration: permissionlessRegistration,
	}
}

func DefaultParams() Params {
	return Params{
		EnableErc20: true,
		// SA-H21 audit-2026-05-29: permissionless ERC20 registration default-off.
		// Combined with SA-C1 (zero-fee post-upgrade) this was free-spam → bank
		// metadata bloat + Symbol Unicode-confusable phishing surface + dynamic
		// precompile registry pollution. Mainnet must enable via gov prop only
		// after Symbol-uniqueness check + per-tx registration fee are wired.
		PermissionlessRegistration: false,
	}
}
