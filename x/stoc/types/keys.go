package types

const (
	// ModuleName defines the module name
	ModuleName = "stoc"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_stoc"
)

var (
	ParamsKey = []byte("p_stoc")
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}
