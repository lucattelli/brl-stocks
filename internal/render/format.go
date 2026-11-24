// Package render turns rows into text: the one-shot table, the JSON
// document, and the number formats of each currency (D46, D51, D64-D66).
package render

import (
	"math"
	"strconv"
	"strings"
)

// number formats x with the given decimals: Brazilian style (comma for
// decimals, dot for thousands) or US style. Values are rounded half away
// from zero, on the decimal digits of the number as written (D66).
func number(x float64, decimals int, brazilian bool) string {
	digits := strconv.FormatFloat(math.Abs(x), 'f', -1, 64)
	whole, frac, _ := strings.Cut(digits, ".")
	if len(frac) > decimals {
		up := frac[decimals] >= '5'
		all := []byte(whole + frac[:decimals])
		if up {
			i := len(all) - 1
			for ; i >= 0; i-- {
				if all[i] == '9' {
					all[i] = '0'
					continue
				}
				all[i]++
				break
			}
			if i < 0 {
				all = append([]byte{'1'}, all...)
			}
		}
		whole, frac = string(all[:len(all)-decimals]), string(all[len(all)-decimals:])
	}
	frac += strings.Repeat("0", decimals-len(frac))
	thousands, point := ",", "."
	if brazilian {
		thousands, point = ".", ","
	}
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteString(thousands)
		}
		b.WriteRune(c)
	}
	if decimals > 0 {
		b.WriteString(point + frac)
	}
	return b.String()
}

// signed is number with a sign in front. A value of zero, or one that rounds
// to zero, reads "+0,00" (D50, D66). The minus sign is the plain hyphen.
func signed(x float64, decimals int, brazilian bool) string {
	s := number(x, decimals, brazilian)
	if x < 0 && strings.Trim(s, "0.,") != "" {
		return "-" + s
	}
	return "+" + s
}
