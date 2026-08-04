package main

import (
	"strings"
	"testing"
)

func TestParseLaunchHotReloadTUI(t *testing.T) {
	launch, err := parseLaunch([]string{"--hot-reload"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !launch.HotReload || len(launch.Args) != 0 {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestParseLaunchHotReloadServePreservesArguments(t *testing.T) {
	launch, err := parseLaunch([]string{"--hot-reload", "serve", "--port", "3080"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !launch.HotReload || strings.Join(launch.Args, " ") != "serve --port 3080" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestParseLaunchChildBypassesSupervisor(t *testing.T) {
	launch, err := parseLaunch([]string{"serve", "--port", "3080"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if launch.HotReload || strings.Join(launch.Args, " ") != "serve --port 3080" {
		t.Fatalf("launch = %#v", launch)
	}
}

func TestParseLaunchRejectsUnsupportedHotReloadShapes(t *testing.T) {
	for _, args := range [][]string{
		{"--hot-reload", "task", "list"},
		{"--hot-reload", "--hot-reload"},
		{"--hot-reload", "serve", "--hot-reload"},
		{"serve", "--hot-reload"},
	} {
		if _, err := parseLaunch(args, false); err == nil {
			t.Errorf("parseLaunch(%v) succeeded", args)
		}
	}
}

func TestHotReloadAppearsInRootAndServeHelp(t *testing.T) {
	for _, path := range [][]string{nil, {"serve"}} {
		out := helpText(path)
		if !strings.Contains(out, "--hot-reload") {
			t.Errorf("help %v missing --hot-reload:\n%s", path, out)
		}
	}
}
