package market

import (
	"fmt"
	"regexp"
	"strings"
)

var b3Shape = regexp.MustCompile(`^[A-Z]{4}[0-9]{1,2}F?$`)

// Parse reads a ticker as the user writes it in the config (D17, D30): four
// letters followed by one or two digits, with an optional trailing F, is B3;
// anything else is US; a "B3:" or "US:" prefix overrides.
func Parse(s string) (Instrument, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	if t == "" || strings.ContainsAny(t, " \t") {
		return Instrument{}, fmt.Errorf("invalid ticker %q", s)
	}
	switch {
	case strings.HasPrefix(t, "B3:"):
		t = t[3:]
		if t == "" {
			return Instrument{}, fmt.Errorf("invalid ticker %q", s)
		}
		return Instrument{B3, t}, nil
	case strings.HasPrefix(t, "US:"):
		t = t[3:]
		if t == "" {
			return Instrument{}, fmt.Errorf("invalid ticker %q", s)
		}
		return Instrument{US, t}, nil
	}
	if b3Shape.MatchString(t) {
		return Instrument{B3, t}, nil
	}
	return Instrument{US, t}, nil
}

// ParseSummary reads a name of the summary line (D42).
func ParseSummary(s string) (Instrument, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "IBOV":
		return Instrument{IDX, "IBOV"}, nil
	case "SP500":
		return Instrument{IDX, "SP500"}, nil
	case "USDBRL":
		return Instrument{FX, "USDBRL"}, nil
	}
	return Instrument{}, fmt.Errorf("unknown summary entry %q (allowed: IBOV, SP500, USDBRL)", s)
}

// Hours says whose trading hours an instrument follows: Ibovespa follows B3,
// S&P 500 follows US, USD/BRL has hours of its own (D50).
func (i Instrument) Hours() Market {
	if i.Market == IDX {
		if i.Symbol == "IBOV" {
			return B3
		}
		return US
	}
	return i.Market
}

// MarshalText and UnmarshalText make an Instrument usable as a JSON map key,
// in both directions (design C3).
func (i Instrument) MarshalText() ([]byte, error) {
	return []byte(string(i.Market) + ":" + i.Symbol), nil
}

func (i *Instrument) UnmarshalText(b []byte) error {
	m, sym, ok := strings.Cut(string(b), ":")
	if !ok || m == "" || sym == "" {
		return fmt.Errorf("invalid instrument %q", b)
	}
	i.Market, i.Symbol = Market(m), sym
	return nil
}

func (i Instrument) String() string { return string(i.Market) + ":" + i.Symbol }
