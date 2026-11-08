package ante

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExceedsPendingCap(t *testing.T) {
	const maxPending = DefaultMaxPendingTxPerWallet
	cases := []struct {
		name        string
		count       int
		alreadyHeld bool
		want        bool
	}{
		{"new tx below cap", maxPending - 1, false, false},
		{"new tx at cap is rejected", maxPending, false, true},
		{"new tx above cap is rejected", maxPending + 5, false, true},
		{"recheck of held tx at cap passes", maxPending, true, false},
		{"recheck of held tx below cap passes", 3, true, false},
		{"held tx above cap (lowered max) still passes", maxPending + 1, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, exceedsPendingCap(tc.count, tc.alreadyHeld, maxPending))
		})
	}
}
