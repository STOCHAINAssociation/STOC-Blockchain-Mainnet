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
		// Permissionless ERC20 registration is off by default. Enabled with
		// zero-fee registration it is free spam → bank metadata bloat + Symbol
		// Unicode-confusable phishing surface + dynamic precompile registry
		// pollution. Enable via governance only after a Symbol-uniqueness check
		// and a per-tx registration fee are in place.
		PermissionlessRegistration: false,
	}
}
