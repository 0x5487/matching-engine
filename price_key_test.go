package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"
)

// TestPriceKeyIsTheSameForEqualValues: the map key of a price depends only on its
// value. Trailing zeros, and a coefficient too large for 128 bits, do not change it.
func TestPriceKeyIsTheSameForEqualValues(t *testing.T) {
	t.Parallel()

	const (
		hugeInt       = "1000000000000000000000000000000000000000000001" // 46 digits, past 128 bits
		hugeIntZeros  = "1000000000000000000000000000000000000000000001.000000"
		hugeMinusOne  = "1000000000000000000000000000000000000000000000"
		wideWrittenIn = "1000000000000000000000000000000.0000000000000000000" // 49-digit coefficient, value 10^30
		wideFits      = "1000000000000000000000000000000"                     // same value, fits 128 bits
	)
	cases := []struct {
		name string
		a, b string
		same bool
	}{
		{"trailing zeros after a point", "100", "100.00", true},
		{"more trailing zeros", "0.5", "0.500000", true},
		{"one digit apart", "100", "100.1", false},
		{"same digits, different scale", "0.1", "0.01", false},
		{"zero written two ways", "0", "0.00", true},
		{"a huge price and its trailing-zero form", hugeInt, hugeIntZeros, true},
		{"two different huge prices", hugeInt, hugeMinusOne, false},
		{"a value above 128 bits written wide and the same value that fits", wideWrittenIn, wideFits, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := priceKeyOf(udecimal.MustParse(tc.a))
			b := priceKeyOf(udecimal.MustParse(tc.b))
			if tc.same {
				require.Equal(t, a, b, "%s and %s are one price", tc.a, tc.b)
			} else {
				require.NotEqual(t, a, b, "%s and %s are different prices", tc.a, tc.b)
			}
		})
	}
}

// TestPriceKeyAllocatesNothingWithinRange: a price the book holds in 128 bits gets
// its key without allocating, since every order and every log goes through it.
func TestPriceKeyAllocatesNothingWithinRange(t *testing.T) {
	price := udecimal.MustParse("100.2500")
	var sink priceKey
	allocs := testing.AllocsPerRun(1000, func() {
		sink = priceKeyOf(price)
	})
	require.Zero(t, allocs)
	require.Equal(t, priceKeyOf(udecimal.MustParse("100.25")), sink)
}
