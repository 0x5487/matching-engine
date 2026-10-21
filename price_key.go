package match

import (
	"math/bits"

	"github.com/quagmt/udecimal"
)

// priceKey is the map key of a price level. Two prices that are equal in value have
// equal keys whatever their written form: 100, 100.0 and 100.000 share one key,
// because trailing zeros are removed from the coefficient before the key is formed.
// The key is exact, not rounded, and needs no market precision.
//
// Prices stored on orders and logs keep the caller's written form; only the key is
// canonical. Ordering in the skiplists and the treemaps already compares by value,
// so the key is needed only where a map is keyed by price.
type priceKey struct {
	neg  bool
	hi   uint64
	lo   uint64
	prec uint8
	// text is set only for a price whose coefficient needs more than 128 bits,
	// which udecimal keeps in a big.Int; see bigPriceKey.
	text string
}

// priceKeyOf returns the canonical key of p. For a coefficient within 128 bits it
// does not allocate.
func priceKeyOf(p udecimal.Decimal) priceKey {
	neg, hi, lo, prec, ok := p.ToHiLo()
	if !ok {
		return bigPriceKey(p)
	}
	return trimmedKey(neg, hi, lo, prec)
}

// decimalBase is the radix of the coefficient's trailing zeros.
const decimalBase = 10

// trimmedKey removes trailing zeros from the 128-bit coefficient hi:lo, scaled by
// 10^-prec, one digit at a time while the scale is positive.
func trimmedKey(neg bool, hi, lo uint64, prec uint8) priceKey {
	for prec > 0 {
		qHi, r := bits.Div64(0, hi, decimalBase)
		qLo, rem := bits.Div64(r, lo, decimalBase)
		if rem != 0 {
			break
		}
		hi, lo = qHi, qLo
		prec--
	}
	return priceKey{neg: neg, hi: hi, lo: lo, prec: prec}
}

// bigPriceKey keys a price whose coefficient udecimal stores in a big.Int. udecimal
// has no allocation-free way to reach that coefficient, so the canonical text, which
// String produces without trailing zeros, is used. The text is parsed back first: a
// value whose canonical form fits 128 bits takes the same key as its in-range
// spelling, so equal prices never get two keys. This path allocates; it is reached
// only by a price with more than about 38 significant digits.
func bigPriceKey(p udecimal.Decimal) priceKey {
	text := p.String()
	if c, err := udecimal.Parse(text); err == nil {
		if neg, hi, lo, prec, ok := c.ToHiLo(); ok {
			return trimmedKey(neg, hi, lo, prec)
		}
	}
	return priceKey{text: text}
}
