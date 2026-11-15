// Package mqtt provides a single MQTT v5 connection without automatic reconnect.
package mqtt

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eclipse/paho.golang/packets"
	"github.com/eclipse/paho.golang/paho"
)

const operationTimeout = 5 * time.Second

// Options configures one connection. Credentials are not retained for retries.
type Options struct {
	Broker            string
	ClientID          string
	Username          string
	Password          string
	AvailabilityTopic string
	BirthTopic        string
	// OnBirth coalesces online birth messages off the network reader. It must return promptly.
	OnBirth func()
}

// Client publishes retained state on one connection. The caller owns retries.
type Client struct {
	protocol *paho.Client
	conn     net.Conn
}

// Dial opens a clean session. Its context bounds setup, not the connection lifetime.
// Dial, Publish and Close impose a five-second maximum even without a caller deadline.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	u, err := brokerURL(opts.Broker)
	if err != nil {
		return nil, err
	}
	if opts.ClientID == "" || !validString(opts.ClientID) {
		return nil, errors.New("mqtt: invalid client ID")
	}
	if !validTopic(opts.AvailabilityTopic) || (opts.BirthTopic != "" && !validTopic(opts.BirthTopic)) {
		return nil, errors.New("mqtt: invalid availability or birth topic")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var conn net.Conn
	if u.Scheme == "mqtts" {
		conn, err = (&tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", u.Host)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", u.Host)
	}
	if err != nil {
		return nil, operationError(ctx, "connection failed")
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	c := &Client{conn: conn}
	birth := make(chan struct{}, 1)
	birthTopic, onBirth := opts.BirthTopic, opts.OnBirth
	c.protocol = paho.NewClient(paho.ClientConfig{
		Conn:          packets.NewThreadSafeConn(conn),
		PacketTimeout: operationTimeout,
		OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(p paho.PublishReceived) (bool, error) {
			if birthTopic != "" && p.Packet.Topic == birthTopic && string(p.Packet.Payload) == "online" {
				select {
				case birth <- struct{}{}:
				default:
				}
			}
			return true, nil
		}}})
	_, err = c.protocol.Connect(ctx, &paho.Connect{
		ClientID: opts.ClientID, CleanStart: true, KeepAlive: 15,
		Username: opts.Username, Password: []byte(opts.Password),
		UsernameFlag: opts.Username != "", PasswordFlag: opts.Password != "",
		WillMessage: &paho.WillMessage{
			Topic: opts.AvailabilityTopic, Payload: []byte("offline"), QoS: 1, Retain: true,
		},
	})
	if err != nil {
		_ = conn.Close()
		return nil, operationError(ctx, "connect refused or failed")
	}
	if opts.BirthTopic != "" {
		ack, err := c.protocol.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: opts.BirthTopic, QoS: 1}}})
		if err != nil || len(ack.Reasons) != 1 || ack.Reasons[0] > 1 {
			_ = conn.Close()
			return nil, operationError(ctx, "birth subscription refused or failed")
		}
	}
	if onBirth != nil {
		go func() {
			for {
				select {
				case <-c.Done():
					return
				case <-birth:
					onBirth()
				}
			}
		}()
	}
	return c, nil
}

// Publish sends retained QoS1 state and waits for the broker's acknowledgement.
// Cancellation of in-flight I/O closes the connection, discarding pending state.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer stop()
	if !validTopic(topic) {
		return errors.New("mqtt: invalid publish topic")
	}
	_, err := c.protocol.Publish(ctx, &paho.Publish{Topic: topic, Payload: payload, QoS: 1, Retain: true})
	if err != nil {
		if ctx.Err() != nil {
			_ = c.conn.Close()
		}
		return operationError(ctx, "publish failed")
	}
	return nil
}

// Close disconnects without publishing offline; availability belongs to the service.
func (c *Client) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		_ = c.conn.Close()
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer stop()
	if err := c.protocol.Disconnect(&paho.Disconnect{}); err != nil {
		return operationError(ctx, "disconnect failed")
	}
	return nil
}

// Done closes when the connection ends, including a graceful Close.
func (c *Client) Done() <-chan struct{} { return c.protocol.Done() }

// Never expose broker reason strings, URLs, payloads or credentials in errors.
func operationError(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("mqtt: %s: %w", message, err)
	}
	return errors.New("mqtt: " + message)
}

func brokerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "mqtt" && u.Scheme != "mqtts") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, errors.New("mqtt: invalid broker URL")
	}
	port := u.Port()
	if port == "" {
		if strings.HasSuffix(u.Host, ":") {
			return nil, errors.New("mqtt: invalid broker port")
		}
		port = "1883"
		if u.Scheme == "mqtts" {
			port = "8883"
		}
	} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, errors.New("mqtt: invalid broker port")
	}
	u.Host = net.JoinHostPort(u.Hostname(), port)
	return u, nil
}

func validString(s string) bool {
	return len(s) <= 65535 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func validTopic(s string) bool { return s != "" && validString(s) && !strings.ContainsAny(s, "+#") }
