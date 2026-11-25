package mqtt_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	pahopackets "github.com/eclipse/paho.golang/packets"
	brokerpackets "github.com/mochi-mqtt/server/v2/packets"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	transport "github.com/cpave3/legato/internal/engine/mqtt"
	"github.com/eclipse/paho.golang/paho"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func startBroker(t *testing.T) (*broker.Server, string) {
	t.Helper()
	return startBrokerHook(t, new(auth.AllowHook))
}

func startBrokerHook(t *testing.T, hook broker.Hook) (*broker.Server, string) {
	t.Helper()
	return startBrokerTLS(t, hook, nil)
}

func startBrokerTLS(t *testing.T, hook broker.Hook, config *tls.Config) (*broker.Server, string) {
	t.Helper()
	s := broker.New(&broker.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := s.AddHook(hook, nil); err != nil {
		t.Fatal(err)
	}
	l := listeners.NewTCP(listeners.Config{ID: "test", Address: "127.0.0.1:0", TLSConfig: config})
	if err := s.AddListener(l); err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Mochi's Close nests read locks while disconnects delete clients.
		// Drain them first so a pending writer cannot deadlock that close.
		for _, client := range s.Clients.GetAll() {
			client.Stop(net.ErrClosed)
		}
		deadline := time.Now().Add(5 * time.Second)
		for s.Clients.Len() != 0 {
			if time.Now().After(deadline) {
				t.Fatal("broker clients did not exit before close")
			}
			time.Sleep(time.Millisecond)
		}
		_ = s.Close()
	})
	return s, "mqtt://" + l.Address()
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func observer(t *testing.T, address, topic string) (*paho.Client, <-chan *paho.Publish) {
	t.Helper()
	conn, err := net.Dial("tcp", address[len("mqtt://"):])
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan *paho.Publish, 20)
	c := paho.NewClient(paho.ClientConfig{Conn: conn, OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(p paho.PublishReceived) (bool, error) { messages <- p.Packet; return true, nil }}})
	if _, err := c.Connect(testContext(t), &paho.Connect{ClientID: "observer-" + topic, CleanStart: true, KeepAlive: 15}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Disconnect(&paho.Disconnect{}) })
	if _, err := c.Subscribe(testContext(t), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: topic, QoS: 1}}}); err != nil {
		t.Fatal(err)
	}
	// Mochi sends SUBACK before it looks up the retained messages for the new
	// subscription. A publication made in between would arrive twice: live,
	// and again as retained. Mochi handles one connection's packets in order,
	// so a second SUBACK means that lookup has finished.
	if _, err := c.Subscribe(testContext(t), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "test/subscribe-barrier", QoS: 0}}}); err != nil {
		t.Fatal(err)
	}
	return c, messages
}

func receive(t *testing.T, messages <-chan *paho.Publish) *paho.Publish {
	t.Helper()
	select {
	case p := <-messages:
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("no MQTT publication")
		return nil
	}
}

func publishBarrier(t *testing.T, c *transport.Client) {
	t.Helper()
	// Mochi sends PUBACK before broadcasting to subscribers. A subsequent
	// packet on the same connection ensures that broadcast has finished.
	if err := c.Publish(testContext(t), "test/publish-barrier", []byte("complete")); err != nil {
		t.Fatal(err)
	}
}

func TestPublishRetainedQoS1(t *testing.T) {
	_, address := startBroker(t)
	ctx := testContext(t)
	c, err := transport.Dial(ctx, transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	for topic, payload := range map[string]string{"legato/discovery/config": "{\"name\":\"Legato\"}", "legato/state": "7"} {
		if err := c.Publish(ctx, topic, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		publishBarrier(t, c)
		_, messages := observer(t, address, topic)
		p := receive(t, messages)
		if p.Topic != topic || string(p.Payload) != payload || !p.Retain || p.QoS != 1 {
			t.Fatalf("unexpected retained publication: %+v", p)
		}
	}
}

func TestBirthOnlineTriggersRepublish(t *testing.T) {
	_, address := startBroker(t)
	calls := make(chan struct{}, 4)
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability", BirthTopic: "homeassistant/status", OnBirth: func() { calls <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	sender, _ := observer(t, address, "unrelated")
	for _, message := range []struct {
		topic, payload string
		want           bool
	}{{"homeassistant/status", "offline", false}, {"other/status", "online", false}, {"homeassistant/status", "online", true}} {
		if _, err := sender.Publish(testContext(t), &paho.Publish{Topic: message.topic, Payload: []byte(message.payload), QoS: 1}); err != nil {
			t.Fatal(err)
		}
		if message.want {
			select {
			case <-calls:
			case <-time.After(time.Second):
				t.Fatal("birth callback not called")
			}
		} else {
			select {
			case <-calls:
				t.Fatal("unexpected callback")
			case <-time.After(30 * time.Millisecond):
			}
		}
	}
}

func TestConnectionLossPublishesRetainedWill(t *testing.T) {
	s, address := startBroker(t)
	_, messages := observer(t, address, "legato/availability")
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(testContext(t), "legato/availability", []byte("online")); err != nil {
		t.Fatal(err)
	}
	if p := receive(t, messages); string(p.Payload) != "online" {
		t.Fatalf("unexpected availability: %s", p.Payload)
	}
	remote, ok := s.Clients.Get("publisher")
	if !ok {
		t.Fatal("publisher not connected")
	}
	if err := remote.Net.Conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("loss not signalled")
	}
	p := receive(t, messages)
	if string(p.Payload) != "offline" || p.QoS != 1 {
		t.Fatalf("unexpected will: %+v", p)
	}
	_, late := observer(t, address, "legato/availability")
	p = receive(t, late)
	if string(p.Payload) != "offline" || !p.Retain || p.QoS != 1 {
		t.Fatalf("will not retained: %+v", p)
	}
	// Transport never reconnects behind the service's back.
	if err := c.Publish(testContext(t), "legato/state", []byte("8")); err == nil {
		t.Fatal("publish after loss succeeded")
	}
}

func TestGracefulCloseLeavesServiceAvailability(t *testing.T) {
	_, address := startBroker(t)
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(testContext(t), "legato/availability", []byte("service-owned")); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("close not signalled")
	}
	_, messages := observer(t, address, "legato/availability")
	if p := receive(t, messages); string(p.Payload) != "service-owned" {
		t.Fatalf("close changed availability: %s", p.Payload)
	}
}

type passwordHook struct{ broker.HookBase }

func (*passwordHook) ID() string { return "password" }
func (*passwordHook) Provides(b byte) bool {
	return b == broker.OnConnectAuthenticate || b == broker.OnACLCheck
}
func (*passwordHook) OnConnectAuthenticate(_ *broker.Client, p brokerpackets.Packet) bool {
	return bytes.Equal(p.Connect.Username, []byte("legato-user")) && bytes.Equal(p.Connect.Password, []byte("secret-password"))
}
func (*passwordHook) OnACLCheck(_ *broker.Client, _ string, _ bool) bool { return true }

func TestAuthentication(t *testing.T) {
	_, address := startBrokerHook(t, new(passwordHook))
	opts := transport.Options{Broker: address, ClientID: "publisher", Username: "legato-user", Password: "wrong-secret", AvailabilityTopic: "legato/availability"}
	if c, err := transport.Dial(testContext(t), opts); err == nil {
		_ = c.Close(testContext(t))
		t.Fatal("bad password accepted")
	} else if strings.Contains(err.Error(), opts.Password) || strings.Contains(err.Error(), opts.Username) {
		t.Fatalf("credentials in error: %v", err)
	}
	opts.Password = "secret-password"
	c, err := transport.Dial(testContext(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(testContext(t), "legato/state", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
}

func testCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test broker"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestTLSVerification(t *testing.T) {
	// Go caches system roots process-wide. Isolate the test trust store, including
	// repeated test runs and packages whose tests have already loaded roots.
	if os.Getenv("LEGATO_MQTT_TLS_TEST") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(testContext(t), executable, "-test.run=^TestTLSVerification$")
		cmd.Env = append(os.Environ(), "LEGATO_MQTT_TLS_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("TLS verification: %v\n%s", err, output)
		}
		return
	}
	trusted, root := testCertificate(t)
	rootFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(rootFile, root, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", rootFile)
	t.Setenv("SSL_CERT_DIR", t.TempDir())
	_, address := startBrokerTLS(t, new(auth.AllowHook), &tls.Config{Certificates: []tls.Certificate{trusted}, MinVersion: tls.VersionTLS12})
	opts := transport.Options{Broker: strings.Replace(address, "mqtt://", "mqtts://", 1), ClientID: "tls-publisher", AvailabilityTopic: "legato/availability"}
	c, err := transport.Dial(testContext(t), opts)
	if err != nil {
		t.Fatalf("trusted TLS: %v", err)
	}
	if err := c.Publish(testContext(t), "legato/state", []byte("42")); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	opts.Broker = strings.Replace(opts.Broker, "127.0.0.1", "localhost", 1)
	if c, err := transport.Dial(testContext(t), opts); err == nil {
		_ = c.Close(testContext(t))
		t.Fatal("wrong hostname accepted")
	}
	untrusted, _ := testCertificate(t)
	_, address = startBrokerTLS(t, new(auth.AllowHook), &tls.Config{Certificates: []tls.Certificate{untrusted}, MinVersion: tls.VersionTLS12})
	opts.Broker = strings.Replace(address, "mqtt://", "mqtts://", 1)
	if c, err := transport.Dial(testContext(t), opts); err == nil {
		_ = c.Close(testContext(t))
		t.Fatal("untrusted certificate accepted")
	}
}

func TestDialCancellation(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = transport.Dial(ctx, transport.Options{Broker: "mqtt://" + l.Addr().String(), ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	conn := <-accepted
	defer conn.Close()
}

func TestInvalidOptions(t *testing.T) {
	for _, address := range []string{"", "http://localhost:1883", "mqtt://user:secret@localhost:1883", "mqtt://localhost:1883/path", "mqtt://localhost:1883?", "mqtt://localhost:1883#fragment", "mqtt://localhost:0", "mqtt://localhost:65536", "mqtt://localhost:bad", "mqtt://:1883"} {
		t.Run(address, func(t *testing.T) {
			_, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability"})
			if err == nil || !strings.Contains(err.Error(), "invalid broker") {
				t.Fatalf("want invalid broker, got %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("URL credential leaked")
			}
		})
	}
}

func TestPublishCancellationInterruptsBlockedSocketWrite(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	wire := make(chan *pahopackets.Connect, 1)
	serverErr := make(chan error, 1)
	release := make(chan struct{})
	defer close(release)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		packet, err := pahopackets.ReadPacket(conn)
		if err != nil {
			serverErr <- err
			return
		}
		wire <- packet.Content.(*pahopackets.Connect)
		_, err = (&pahopackets.Connack{Properties: &pahopackets.Properties{}}).WriteTo(conn)
		if err != nil {
			serverErr <- err
			return
		}
		<-release // Deliberately never read or acknowledge the publish.
	}()
	c, err := transport.Dial(testContext(t), transport.Options{Broker: "mqtt://" + l.Addr().String(), ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case packet := <-wire:
		if !packet.CleanStart || packet.KeepAlive != 15 || packet.ProtocolVersion != 5 || (packet.Properties.SessionExpiryInterval != nil && *packet.Properties.SessionExpiryInterval != 0) {
			t.Fatalf("not a clean v5 session with 15s keepalive: %+v", packet)
		}
	case err := <-serverErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("CONNECT not observed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = c.Publish(ctx, "legato/state", make([]byte, 16<<20))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("canceled operation left a live connection")
	}
}

func TestCloseWithExpiredContextStillReleasesConnection(t *testing.T) {
	_, address := startBroker(t)
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Close leaked canceled connection")
	}
}

func TestDialHasDefaultTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	result := make(chan error, 1)
	go func() {
		c, err := transport.Dial(context.Background(), transport.Options{Broker: "mqtt://" + l.Addr().String(), ClientID: "publisher", AvailabilityTopic: "legato/availability"})
		if c != nil {
			_ = c.Close(context.Background())
		}
		result <- err
	}()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("not connected")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want deadline exceeded, got %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Dial with no caller deadline was not bounded")
	}
}

func TestPublishEmptyClearsRetainedState(t *testing.T) {
	_, address := startBroker(t)
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	if err := c.Publish(testContext(t), "legato/attributes", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(testContext(t), "legato/attributes", nil); err != nil {
		t.Fatal(err)
	}
	publishBarrier(t, c)
	_, messages := observer(t, address, "legato/attributes")
	// Retained attributes would be queued for the observer while it
	// subscribed, ahead of this live sentinel.
	if err := c.PublishTransient(testContext(t), "legato/attributes", []byte("sentinel")); err != nil {
		t.Fatal(err)
	}
	if p := receive(t, messages); string(p.Payload) != "sentinel" {
		t.Fatalf("retained attributes survived clearing: %+v", p)
	}
}

func TestRetainedBirthCanRepublishFromCallback(t *testing.T) {
	_, address := startBroker(t)
	sender, _ := observer(t, address, "unused")
	if _, err := sender.Publish(testContext(t), &paho.Publish{Topic: "homeassistant/status", Payload: []byte("online"), QoS: 1, Retain: true}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan *transport.Client, 1)
	published := make(chan error, 1)
	ctx := testContext(t)
	c, err := transport.Dial(ctx, transport.Options{Broker: address, ClientID: "publisher", AvailabilityTopic: "legato/availability", BirthTopic: "homeassistant/status", OnBirth: func() { c := <-ready; published <- c.Publish(ctx, "legato/state", []byte("9")) }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	ready <- c
	select {
	case err := <-published:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("birth republish blocked")
	}
	_, messages := observer(t, address, "legato/state")
	if p := receive(t, messages); string(p.Payload) != "9" || !p.Retain || p.QoS != 1 {
		t.Fatalf("birth republish not retained QoS1: %+v", p)
	}
}

// lateFanOut widens a gap in mochi: it sends a publisher its PUBACK before it
// selects the subscribers to send the publication to. OnQosComplete runs in
// between, so the first QoS 1 publication reaches whoever subscribes in the
// next 20 ms. A test that publishes, then subscribes, must wait for the
// fan-out itself.
type lateFanOut struct {
	auth.AllowHook
	once sync.Once
}

func (h *lateFanOut) Provides(b byte) bool {
	return b == broker.OnQosComplete || h.AllowHook.Provides(b)
}

func (h *lateFanOut) OnQosComplete(*broker.Client, brokerpackets.Packet) {
	h.once.Do(func() { time.Sleep(20 * time.Millisecond) })
}

func TestCommandsSkipReplayPreserveLiveRetainAndBoundPayload(t *testing.T) {
	_, address := startBrokerHook(t, new(lateFanOut))
	sender, _ := observer(t, address, "unused-command")
	send := func(payload string, retained bool) {
		t.Helper()
		if _, err := sender.Publish(testContext(t), &paho.Publish{Topic: "legato/theme/set", Payload: []byte(payload), Retain: retained, QoS: 1}); err != nil {
			t.Fatal(err)
		}
	}
	send("replay", true)
	// Mochi handles one connection's packets in order, so this SUBACK comes
	// after the fan-out of "replay": the control client cannot receive it live.
	if _, err := sender.Subscribe(testContext(t), &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "test/subscribe-barrier", QoS: 0}}}); err != nil {
		t.Fatal(err)
	}
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "control", AvailabilityTopic: "legato/theme/availability", CommandTopic: "legato/theme/set"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	// A replayed retained command would be queued for the client while it
	// subscribed, ahead of any live one, so the first command proves none came.
	send("live retained", true)
	select {
	case command := <-c.Commands():
		if string(command.Payload) == "replay" {
			t.Fatal("replayed retained command")
		}
		if !command.Retained || string(command.Payload) != "live retained" {
			t.Fatalf("lost retain flag: %+v", command)
		}
	case <-time.After(time.Second):
		t.Fatal("command missing")
	}
	send(strings.Repeat("x", 2049), false)
	select {
	case command := <-c.Commands():
		if command.Retained || len(command.Payload) != 0 {
			t.Fatalf("oversized command retained payload bytes: %+v", command)
		}
	case <-time.After(time.Second):
		t.Fatal("oversized command refusal missing")
	}
	send("valid", false)
	select {
	case command := <-c.Commands():
		if command.Retained || string(command.Payload) != "valid" {
			t.Fatalf("unexpected command: %+v", command)
		}
	case <-time.After(time.Second):
		t.Fatal("valid command missing")
	}
	if err := c.PublishTransient(testContext(t), "legato/theme/result", []byte(`{"code":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	// The QoS 0 result has no acknowledgement; the barrier's PUBACK means the broker
	// has handled it, and would have retained it, before the observer subscribes.
	publishBarrier(t, c)
	_, results := observer(t, address, "legato/theme/result")
	// A retained result would be queued for the observer while it subscribed,
	// ahead of this live one.
	if err := c.PublishTransient(testContext(t), "legato/theme/result", []byte("sentinel")); err != nil {
		t.Fatal(err)
	}
	if result := receive(t, results); string(result.Payload) != "sentinel" {
		t.Fatalf("result retained: %q", result.Payload)
	}
}

func TestCommandQueueCannotBlockNetworkReader(t *testing.T) {
	_, address := startBroker(t)
	birth := make(chan struct{}, 1)
	c, err := transport.Dial(testContext(t), transport.Options{Broker: address, ClientID: "bounded-reader", AvailabilityTopic: "test/theme/availability", CommandTopic: "test/theme/set", BirthTopic: "ha/status", OnBirth: func() { birth <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	sender, _ := observer(t, address, "unused-queue")
	// Deliberately never consume commands while the broker sends the burst.
	for i := 0; i < 30; i++ {
		if _, err := sender.Publish(testContext(t), &paho.Publish{Topic: "test/theme/set", Payload: []byte("command"), QoS: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sender.Publish(testContext(t), &paho.Publish{Topic: "ha/status", Payload: []byte("online"), QoS: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-birth:
	case <-time.After(time.Second):
		t.Fatal("commands blocked birth processing on reader")
	}
	count := 0
	for {
		select {
		case <-c.Commands():
			count++
		default:
			if count != 5 {
				t.Fatalf("queue size: %d", count)
			}
			return
		}
	}
}

func TestPublishConnectionLossDoesNotWaitForMissingAcknowledgement(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		if _, err := pahopackets.ReadPacket(conn); err != nil {
			serverDone <- err
			return
		}
		if _, err := (&pahopackets.Connack{Properties: &pahopackets.Properties{}}).WriteTo(conn); err != nil {
			serverDone <- err
			return
		}
		for {
			packet, err := pahopackets.ReadPacket(conn)
			if err != nil {
				serverDone <- err
				return
			}
			switch packet.Type {
			case pahopackets.PINGREQ:
				if _, err := (&pahopackets.Pingresp{}).WriteTo(conn); err != nil {
					serverDone <- err
					return
				}
			case pahopackets.PUBLISH:
				// Drop only after the QoS1 publish, without sending PUBACK.
				conn.Close()
				serverDone <- nil
				return
			default:
				serverDone <- errors.New("expected PUBLISH or PINGREQ")
				return
			}
		}
	}()
	client, err := transport.Dial(testContext(t), transport.Options{Broker: "mqtt://" + listener.Addr().String(), ClientID: "publisher", AvailabilityTopic: "test/availability"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = client.Publish(ctx, "test/state", []byte("current"))
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("publish must fail on connection loss, not wait for caller deadline: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
