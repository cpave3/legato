package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/paho"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// The child enters the real application, with every Legato broker/session
// variable removed and all persistent and runtime paths inside its scratch root.
func TestMQTTScratchEntrypoints(t *testing.T) {
	if mode := os.Getenv("LEGATO_MQTT_TEST_CHILD"); mode != "" {
		// TestMain intentionally clears LEGATO_CONFIG before every test process.
		// Restore only the explicitly supplied scratch path, never session vars.
		if err := os.Setenv("LEGATO_CONFIG", os.Getenv("LEGATO_MQTT_TEST_CONFIG")); err != nil {
			t.Fatal(err)
		}
		args := []string(nil)
		if mode == "serve" {
			args = []string{"serve", "--port", os.Getenv("LEGATO_MQTT_TEST_PORT")}
		} else if mode == "seed" {
			args = []string{"columns", "sync"}
		}
		if code := runApplication(args); code != 0 {
			t.Fatalf("scratch application exit %d", code)
		}
		return
	}
	if testing.Short() {
		t.Skip("isolated process acceptance")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required for scratch TUI acceptance")
	}
	for _, mode := range []string{"serve", "tui"} {
		t.Run(mode, func(t *testing.T) { mqttScratchEntrypoint(t, mode) })
	}
}

func mqttScratchEntrypoint(t *testing.T, mode string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "lgmq-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for _, dir := range []string{"home", "runtime", "tmux", "data", "config", "cache"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	server := broker.New(&broker.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	listener := listeners.NewTCP(listeners.Config{ID: "scratch", Address: "127.0.0.1:0"})
	if err := server.AddListener(listener); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeMQTTTestBroker(t, server) })
	connection, err := net.Dial("tcp", listener.Address())
	if err != nil {
		t.Fatal(err)
	}
	publications := make(chan *paho.Publish, 128)
	observer := paho.NewClient(paho.ClientConfig{Conn: connection, OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(p paho.PublishReceived) (bool, error) { publications <- p.Packet; return true, nil }}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := observer.Connect(ctx, &paho.Connect{ClientID: "scratch-observer", CleanStart: true, KeepAlive: 15}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Disconnect(&paho.Disconnect{}) })
	if _, err := observer.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "#", QoS: 1}}}); err != nil {
		t.Fatal(err)
	}
	configuration := fmt.Sprintf("db:\n  path: %s/legato.db\ndata_dir: %s/data\nboard:\n  columns:\n    - name: Backlog\n    - name: Doing\n    - name: Review\n    - name: Done\nweb:\n  enabled: false\nintegrations:\n  mqtt:\n    enabled: true\n    broker: mqtt://%s\n    base_topic: scratch/legato\n", root, root, listener.Address())
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "LEGATO_") || strings.HasPrefix(key, "XDG_") || key == "HOME" || key == "TMUX" || key == "TMUX_TMPDIR" || key == "CLAUDE_CODE_PLUGIN_DIRS" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "HOME="+filepath.Join(root, "home"), "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"), "XDG_DATA_HOME="+filepath.Join(root, "data"), "XDG_RUNTIME_DIR="+filepath.Join(root, "runtime"), "TMUX_TMPDIR="+filepath.Join(root, "tmux"), "LEGATO_CONFIG="+configPath, "LEGATO_MQTT_TEST_CONFIG="+configPath, "LEGATO_MQTT_TEST_CHILD="+mode, "TERM=xterm-256color")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stop func()
	if mode == "serve" {
		socket, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := fmt.Sprint(socket.Addr().(*net.TCPAddr).Port)
		_ = socket.Close()
		command := exec.Command(executable, "-test.run=^TestMQTTScratchEntrypoints$")
		command.Dir = root
		command.Env = append(env, "LEGATO_MQTT_TEST_PORT="+port)
		logFile, err := os.Create(filepath.Join(root, "process.log"))
		if err != nil {
			t.Fatal(err)
		}
		defer logFile.Close()
		command.Stdout = logFile
		command.Stderr = logFile
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		stop = func() {
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("scratch serve exit: %v", err)
				}
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Error("scratch serve shutdown timed out")
			}
		}
	} else {
		seed := exec.Command(executable, "-test.run=^TestMQTTScratchEntrypoints$")
		seed.Dir = root
		seed.Env = append(env, "LEGATO_MQTT_TEST_CHILD=seed")
		if output, err := seed.CombinedOutput(); err != nil {
			t.Fatalf("seed scratch columns: %v: %s", err, output)
		}
		socketName := "scratch"
		tmux := func(args ...string) error {
			command := exec.Command("tmux", append([]string{"-L", socketName}, args...)...)
			command.Env = env
			command.Dir = root
			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf("%v: %w: %s", args, err, output)
			}
			return nil
		}
		exitPath := filepath.Join(root, "exit.status")
		childCommand := fmt.Sprintf("%q -test.run=^TestMQTTScratchEntrypoints$; code=$?; printf '%%s' \"$code\" > %q", executable, exitPath)
		if err := tmux("new-session", "-d", "-s", "mqtt", "-x", "100", "-y", "35", childCommand); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tmux("kill-server") })
		if err := tmux("pipe-pane", "-o", "-t", "mqtt", fmt.Sprintf("cat > %q", filepath.Join(root, "process.log"))); err != nil {
			t.Fatal(err)
		}
		stop = func() {
			if err := tmux("send-keys", "-t", "mqtt", "q"); err != nil {
				t.Error(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if status, err := os.ReadFile(exitPath); err == nil && len(status) > 0 {
					if string(status) != "0" {
						t.Errorf("scratch TUI exit: %s", status)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Error("scratch TUI shutdown timed out")
		}
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			stop()
		}
		if t.Failed() {
			if data, err := os.ReadFile(filepath.Join(root, "process.log")); err == nil {
				t.Log(string(data))
			}
		}
	})
	discoveries := map[string]bool{}
	states := map[string]string{}
	await := func(payload string) {
		t.Helper()
		for {
			select {
			case message := <-publications:
				if strings.HasPrefix(message.Topic, "homeassistant/") && strings.HasSuffix(message.Topic, "/config") {
					discoveries[message.Topic] = true
				}
				if strings.HasSuffix(message.Topic, "/state") {
					states[message.Topic] = string(message.Payload)
				}
				if message.Topic == "scratch/legato/availability" && string(message.Payload) == payload {
					return
				}
			case <-ctx.Done():
				t.Fatalf("scratch %s did not publish %s; discovery=%d states=%v", mode, payload, len(discoveries), states)
			}
		}
	}
	await("online")
	if len(discoveries) != 10 || len(states) != 10 {
		t.Fatalf("scratch %s discovery=%d state=%d", mode, len(discoveries), len(states))
	}
	stop()
	stopped = true
	await("offline")
}
