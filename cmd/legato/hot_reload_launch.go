package main

import "fmt"

const hotReloadChildEnv = "LEGATO_HOT_RELOAD_CHILD"

type launchOptions struct {
	Args      []string
	HotReload bool
}

func parseLaunch(args []string, child bool) (launchOptions, error) {
	out := launchOptions{Args: append([]string(nil), args...)}
	for i, arg := range args {
		if arg != "--hot-reload" {
			continue
		}
		if i != 0 {
			return launchOptions{}, fmt.Errorf("--hot-reload must be the first argument")
		}
		if len(args) > 1 && args[1] == "--hot-reload" {
			return launchOptions{}, fmt.Errorf("--hot-reload may only be specified once")
		}
		out.Args = append([]string(nil), args[1:]...)
		if len(out.Args) > 0 && out.Args[0] != "serve" {
			return launchOptions{}, fmt.Errorf("--hot-reload supports only the default TUI or serve")
		}
		out.HotReload = !child
		return out, nil
	}
	return out, nil
}
