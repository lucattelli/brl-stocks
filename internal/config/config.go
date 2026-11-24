// Package config reads the hand-edited config file. It never writes it (D35).
package config

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/lucattelli/brl-stocks/internal/market"
)

// Group is a named section of the watchlist; the first group may have no
// name and holds the tickers outside any group.
type Group struct {
	Name    string
	Tickers []market.Instrument
}

// Config is the watchlist and its settings.
type Config struct {
	Interval time.Duration
	Summary  []market.Instrument
	Groups   []Group
}

const (
	defaultInterval = 60 * time.Second
	minInterval     = 10 * time.Second
	designedFor     = 20
)

var defaultSummary = []market.Instrument{{Market: market.IDX, Symbol: "IBOV"}, {Market: market.IDX, Symbol: "SP500"}, {Market: market.FX, Symbol: "USDBRL"}}

// Example is the built-in watchlist used when there is no config file (D35, D50).
func Example() Config {
	var g Group
	for _, t := range []string{"PETR4", "VALE3", "ITUB4", "AAPL", "MSFT"} {
		in, _ := market.Parse(t)
		g.Tickers = append(g.Tickers, in)
	}
	return Config{Interval: defaultInterval, Summary: defaultSummary, Groups: []Group{g}}
}

type file struct {
	RefreshSeconds *int64   `toml:"refresh_seconds"`
	Summary        []string `toml:"summary"`
	Tickers        []string `toml:"tickers"`
	Group          []struct {
		Name    string   `toml:"name"`
		Tickers []string `toml:"tickers"`
	} `toml:"group"`
}

// Parse reads a config (D42). Warnings do not stop the tool; an error makes
// the config invalid (D45).
func Parse(data []byte) (Config, []string, error) {
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Config{}, nil, err
	}
	var warns []string
	for _, k := range md.Undecoded() {
		warns = append(warns, fmt.Sprintf("unknown config key %q", k.String()))
	}
	c := Config{Interval: defaultInterval, Summary: defaultSummary}
	if f.RefreshSeconds != nil {
		c.Interval = time.Duration(*f.RefreshSeconds) * time.Second
		if c.Interval < minInterval {
			c.Interval = minInterval
			warns = append(warns, "refresh_seconds is below 10; using 10")
		}
	}
	if md.IsDefined("summary") {
		c.Summary = nil
		for _, s := range f.Summary {
			in, err := market.ParseSummary(s)
			if err != nil {
				return Config{}, nil, err
			}
			c.Summary = append(c.Summary, in)
		}
	}
	add := func(name string, tickers []string) error {
		g := Group{Name: name}
		seen := map[market.Instrument]bool{}
		for _, t := range tickers {
			in, err := market.Parse(t)
			if err != nil {
				return err
			}
			if !seen[in] {
				seen[in] = true
				g.Tickers = append(g.Tickers, in)
			}
		}
		c.Groups = append(c.Groups, g)
		return nil
	}
	if len(f.Tickers) > 0 {
		if err := add("", f.Tickers); err != nil {
			return Config{}, nil, err
		}
	}
	for _, g := range f.Group {
		if err := add(g.Name, g.Tickers); err != nil {
			return Config{}, nil, err
		}
	}
	if n := len(c.Rows()); n > designedFor {
		warns = append(warns, fmt.Sprintf("%d tickers; the tool is designed for %d", n, designedFor))
	}
	return c, warns, nil
}

// Rows lists the distinct tickers of the watchlist, in file order.
func (c Config) Rows() []market.Instrument {
	var out []market.Instrument
	seen := map[market.Instrument]bool{}
	for _, g := range c.Groups {
		for _, t := range g.Tickers {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Want lists everything to fetch: the summary entries, then the rows.
func (c Config) Want() []market.Instrument {
	return append(append([]market.Instrument{}, c.Summary...), c.Rows()...)
}
