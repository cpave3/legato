package main

import "fmt"

const hotReloadChildEnv = "LEGATO_HOT_RELOAD_CHILD"

type launchOptions struct {
	Args      []string
	HotReload bool
}

func parseLaunch(args []string, child bool) (launchOptions, error) {
	out := launchOptions{Args: append([]string(nil), args...)}
	flagIndex, count := -1, 0
	for i, arg := range args {
		if arg == "--hot-reload" {
			flagIndex, count = i, count+1
		}
	}
	if count == 0 {
		return out, nil
	}
	if count > 1 {
		return launchOptions{}, fmt.Errorf("--hot-reload may only be specified once")
	}
	if flagIndex != 0 {
		return launchOptions{}, fmt.Errorf("--hot-reload must be the first argument")
	}
	out.Args = append([]string(nil), args[1:]...)
	if len(out.Args) > 0 && out.Args[0] != "serve" {
		return launchOptions{}, fmt.Errorf("--hot-reload supports only the default TUI or serve")
	}
	out.HotReload = !child
	return out, nil
}
