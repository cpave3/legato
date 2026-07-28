package main

import (
	"fmt"
	"strings"
)

func flagPresent(flags map[string]string, name string) bool {
	_, ok := flags[name]
	return ok
}

func parseWorkflowArgs(args []string) (positionals []string, flags map[string]string, lists map[string][]string, err error) {
	flags = map[string]string{}
	lists = map[string][]string{}
	scalar := map[string]bool{"run": true, "step": true, "attempt": true, "status": true, "limit": true}
	repeatable := map[string]bool{"input": true, "output": true, "artifact": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if key, value, ok := strings.Cut(name, "="); ok {
			if repeatable[key] {
				lists[key] = append(lists[key], value)
				continue
			}
			if scalar[key] {
				if _, exists := flags[key]; exists {
					return nil, nil, nil, fmt.Errorf("flag --%s may only be specified once", key)
				}
				flags[key] = value
				continue
			}
			return nil, nil, nil, fmt.Errorf("unknown flag --%s", key)
		}
		if name == "json" || name == "force" {
			if _, exists := flags[name]; exists {
				return nil, nil, nil, fmt.Errorf("flag --%s may only be specified once", name)
			}
			flags[name] = ""
			continue
		}
		if !scalar[name] && !repeatable[name] {
			return nil, nil, nil, fmt.Errorf("unknown flag --%s", name)
		}
		if i+1 >= len(args) {
			return nil, nil, nil, fmt.Errorf("missing value for --%s", name)
		}
		i++
		value := args[i]
		if repeatable[name] {
			lists[name] = append(lists[name], value)
		} else {
			if _, exists := flags[name]; exists {
				return nil, nil, nil, fmt.Errorf("flag --%s may only be specified once", name)
			}
			flags[name] = value
		}
	}
	return
}
