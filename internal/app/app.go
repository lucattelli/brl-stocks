// Package app is the whole tool behind one function, so that tests can drive
// it without a terminal.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucattelli/brl-stocks/internal/cache"
	"github.com/lucattelli/brl-stocks/internal/config"
	"github.com/lucattelli/brl-stocks/internal/deps"
	"github.com/lucattelli/brl-stocks/internal/engine"
	"github.com/lucattelli/brl-stocks/internal/render"
)

// Version is set at build time.
var Version = "dev"

const (
	exitOK       = 0
	exitUsage    = 2  // usage and config errors (D44, D45)
	exitNotBuilt = 70 // a part of the tool that does not exist yet

	printDeadline = 10 * time.Second // a one-shot call waits this long for its fetches (D50)

	usage = `usage: brl-stocks [--config <path>]                     open the full-screen view
       brl-stocks [--config <path>] print [--json] [--refresh]
       brl-stocks --help | --version`
)

type options struct {
	command    string
	configPath string
	json       bool
	refresh    bool
	help       bool
	version    bool
}

func parseArgs(args []string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			o.help = true
		case a == "--version":
			o.version = true
		case a == "--config":
			if i+1 >= len(args) {
				return o, errors.New("--config needs a path")
			}
			i++
			o.configPath = args[i]
		case strings.HasPrefix(a, "--config="):
			o.configPath = strings.TrimPrefix(a, "--config=")
		case a == "print" && o.command == "":
			o.command = "print"
		case a == "--json" && o.command == "print":
			o.json = true
		case a == "--refresh" && o.command == "print":
			o.refresh = true
		default:
			return o, fmt.Errorf("unknown command or flag %q", a)
		}
	}
	return o, nil
}

// Run executes one invocation of brl-stocks. args are the command-line
// arguments without the program name. It returns the exit status.
func Run(ctx context.Context, args []string, d deps.Deps) int {
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().Round(0) }
	}
	if d.After == nil {
		d.After = time.After
	}
	if d.LookupEnv == nil {
		d.LookupEnv = os.LookupEnv
	}
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(d.Stderr, "brl-stocks: %v\n%s\n", err, usage)
		return exitUsage
	}
	switch {
	case o.help:
		fmt.Fprintln(d.Stdout, usage)
		return exitOK
	case o.version:
		fmt.Fprintln(d.Stdout, "brl-stocks", Version)
		return exitOK
	case o.command != "print":
		fmt.Fprintln(d.Stderr, "brl-stocks: the full-screen view is not built yet; use `brl-stocks print`")
		return exitNotBuilt
	}
	return runPrint(ctx, o, d)
}

func runPrint(ctx context.Context, o options, d deps.Deps) int {
	fail := func(reason string) int {
		fmt.Fprintln(d.Stderr, "brl-stocks:", reason)
		if o.json {
			render.ErrorDocument(d.Stdout, d.Now(), reason)
		}
		return exitUsage
	}
	cfg, mtime, notes, err := loadConfig(o.configPath, d)
	if err != nil {
		return fail(err.Error())
	}
	if d.Provider == nil {
		fmt.Fprintln(d.Stderr, "brl-stocks: no data source")
		return exitNotBuilt
	}
	eng := engine.Engine{P: d.Provider, C: cache.Files{Dir: cacheDir(d)}, Now: d.Now, After: d.After}
	res := eng.Cycle(ctx, engine.Request{
		Want: cfg.Want(), Interval: cfg.Interval, ConfigMTime: mtime,
		Force: o.refresh, Deadline: printDeadline, WaitForOthers: true,
	})
	_, noColour := d.LookupEnv("NO_COLOR")
	view := render.View{Cfg: cfg, Snap: res.Snap, Health: res.Health, Now: d.Now(), Colour: d.IsTTY && !noColour}
	if o.json {
		if err := render.JSON(d.Stdout, view); err != nil {
			fmt.Fprintln(d.Stderr, "brl-stocks:", err)
		}
	} else {
		render.Table(d.Stdout, view)
	}
	// The cause of a failure and any warning go to the error stream, one
	// line each, so that standard output stays a table (D19, D50).
	if cause := render.Cause(view); cause != "" {
		fmt.Fprintln(d.Stderr, cause)
	}
	for _, n := range notes {
		fmt.Fprintln(d.Stderr, n)
	}
	if res.Warn != nil && !errors.Is(res.Warn, cache.ErrBusy) {
		fmt.Fprintf(d.Stderr, "quotes cannot be kept, so none were fetched: %s cannot be written\n", cacheDir(d))
	}
	return exitOK
}

func home(d deps.Deps) string {
	h, _ := d.LookupEnv("HOME")
	return h
}

func cacheDir(d deps.Deps) string {
	if x, ok := d.LookupEnv("XDG_CACHE_HOME"); ok && x != "" {
		return filepath.Join(x, "brl-stocks")
	}
	return filepath.Join(home(d), ".cache", "brl-stocks")
}

func defaultConfigPath(d deps.Deps) string {
	if x, ok := d.LookupEnv("XDG_CONFIG_HOME"); ok && x != "" {
		return filepath.Join(x, "brl-stocks", "config.toml")
	}
	return filepath.Join(home(d), ".config", "brl-stocks", "config.toml")
}

// loadConfig reads the config, through any symbolic link, and never writes
// it (D29, D35). With no file at the default path, the built-in example is
// used and a note says where a config belongs.
func loadConfig(explicit string, d deps.Deps) (config.Config, time.Time, []string, error) {
	path := explicit
	if path == "" {
		path = defaultConfigPath(d)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && explicit == "" {
			return config.Example(), time.Time{}, []string{"no config file: showing an example watchlist. Put yours at " + path}, nil
		}
		return config.Config{}, time.Time{}, nil, fmt.Errorf("cannot read the config %s: %v", path, err)
	}
	cfg, warns, err := config.Parse(data)
	if err != nil {
		return config.Config{}, time.Time{}, nil, fmt.Errorf("invalid config %s: %v", path, err)
	}
	var mtime time.Time
	if st, err := os.Stat(path); err == nil {
		mtime = st.ModTime()
	}
	return cfg, mtime, warns, nil
}
