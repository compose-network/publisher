package quic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// Client connects to a QUIC server (publisher or peer sidecar).
type Client interface {
	// Connect establishes a connection to the server.
	Connect(ctx context.Context) error

	// Disconnect closes the connection.
	Disconnect(ctx context.Context) error

	// Send sends a protobuf message to the server.
	Send(ctx context.Context, msg proto.Message) error

	// SendRaw sends raw bytes to the server.
	SendRaw(ctx context.Context, data []byte) error

	// IsConnected returns whether the client is connected.
	IsConnected() bool

	// SetHandler sets the message handler for incoming messages.
	SetHandler(handler RawMessageHandler)

	// ConnectWithRetry attempts to connect with retries.
	ConnectWithRetry(ctx context.Context) error

	// GetConnection returns the underlying connection.
	GetConnection() *Connection
}

type client struct {
	mu           sync.RWMutex
	log          zerolog.Logger
	cfg          ClientConfig
	conn         *Connection
	qconn        quic.Connection
	connected    bool
	handler      RawMessageHandler
	stopCh       chan struct{}
	wg           sync.WaitGroup
	connectMu    sync.Mutex
	reconnMu     sync.Mutex
	reconnecting bool
}

// NewClient creates a new QUIC client.
func NewClient(cfg ClientConfig, log zerolog.Logger) Client {
	return &client{
		log:    log.With().Str("component", "quic.client").Logger(),
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

func (c *client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	c.log.Info().Str("addr", c.cfg.ServerAddr).Msg("Connecting to QUIC server")

	tlsConfig := c.cfg.TLSConfig
	if tlsConfig == nil {
		tlsConfig = InsecureClientTLSConfig()
		c.log.Warn().Msg("Using insecure TLS config for QUIC client")
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout:          c.cfg.IdleTimeout,
		HandshakeIdleTimeout:    c.cfg.HandshakeIdleTimeout,
		KeepAlivePeriod:         clientKeepAlive(c.cfg),
		MaxIncomingStreams:      c.cfg.MaxIncomingStreams,
		MaxIncomingUniStreams:   c.cfg.MaxIncomingUniStreams,
		DisablePathMTUDiscovery: c.cfg.DisablePathMTUDiscovery,
		EnableDatagrams:         c.cfg.EnableDatagrams,
	}

	dialCtx := ctx
	var cancel context.CancelFunc
	if c.cfg.DialTimeout > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, c.cfg.DialTimeout)
		defer cancel()
	}

	qconn, err := quic.DialAddr(dialCtx, c.cfg.ServerAddr, tlsConfig, quicConfig)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	// Send client ID on first stream
	stream, err := qconn.OpenStreamSync(ctx)
	if err != nil {
		qconn.CloseWithError(1, "identification failed")
		return fmt.Errorf("open identification stream: %w", err)
	}

	if err := writeFrame(stream, []byte(c.cfg.ClientID)); err != nil {
		stream.Close()
		qconn.CloseWithError(1, "identification failed")
		return fmt.Errorf("write client ID: %w", err)
	}
	stream.Close()

	c.qconn = qconn
	c.conn = NewConnection(qconn, c.cfg.ClientID, c.cfg.MaxMessageSize)
	c.connected = true
	c.stopCh = make(chan struct{})

	c.log.Info().Str("addr", c.cfg.ServerAddr).Msg("Connected to QUIC server")

	// Start read loop
	c.wg.Add(1)
	go c.readLoop()
	c.wg.Add(1)
	go c.watchConnection(qconn)

	return nil
}

func (c *client) ConnectWithRetry(ctx context.Context) error {
	for i := 0; i < c.cfg.MaxRetries; i++ {
		if err := c.Connect(ctx); err == nil {
			return nil
		}

		c.log.Warn().
			Int("attempt", i+1).
			Int("max", c.cfg.MaxRetries).
			Dur("delay", c.cfg.ReconnectDelay).
			Msg("Connection failed, retrying")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.cfg.ReconnectDelay):
		}
	}

	return fmt.Errorf("failed to connect after %d attempts", c.cfg.MaxRetries)
}

func (c *client) Disconnect(ctx context.Context) error {
	c.mu.Lock()

	if !c.connected {
		c.mu.Unlock()
		return nil
	}

	c.log.Info().Msg("Disconnecting from QUIC server")

	close(c.stopCh)

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.qconn = nil
	c.connected = false
	c.mu.Unlock()

	c.wg.Wait()
	return nil
}

func (c *client) Send(ctx context.Context, msg proto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return c.SendRaw(ctx, data)
}

func (c *client) SendRaw(ctx context.Context, data []byte) error {
	if err := c.ensureConnected(ctx); err != nil {
		return err
	}

	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected to server")
	}

	if err := conn.SendRaw(ctx, data); err != nil {
		if c.shouldReconnect(err) {
			c.markDisconnected(err)
		}
		return err
	}

	return nil
}

func (c *client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *client) SetHandler(handler RawMessageHandler) {
	c.mu.Lock()
	c.handler = handler
	c.mu.Unlock()
}

func (c *client) GetConnection() *Connection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *client) readLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		connected := c.connected
		c.mu.RUnlock()

		if !connected || conn == nil {
			return
		}

		connCtx := conn.Context()
		select {
		case <-c.stopCh:
			return
		case <-connCtx.Done():
			return
		default:
		}

		stream, err := conn.AcceptStream(connCtx)
		if err != nil {
			select {
			case <-c.stopCh:
				return
			case <-connCtx.Done():
				return
			default:
				c.log.Debug().Err(err).Msg("Stream accept failed")
				c.markDisconnected(err)
				return
			}
		}

		c.handleStream(connCtx, stream)
	}
}

func (c *client) watchConnection(qconn quic.Connection) {
	defer c.wg.Done()

	<-qconn.Context().Done()

	select {
	case <-c.stopCh:
		return
	default:
	}

	err := context.Cause(qconn.Context())
	if err == nil {
		err = qconn.Context().Err()
	}
	c.markDisconnected(err)
}

func (c *client) handleStream(ctx context.Context, stream quic.Stream) {
	defer stream.Close()

	data, err := readFrame(stream, c.cfg.MaxMessageSize)
	if err != nil {
		c.log.Debug().Err(err).Msg("Failed to read message")
		return
	}
	if conn := c.GetConnection(); conn != nil {
		conn.RecordActivity()
	}

	c.mu.RLock()
	handler := c.handler
	c.mu.RUnlock()

	if handler != nil {
		if err := handler(ctx, "", data); err != nil {
			c.log.Error().Err(err).Msg("Handler failed")
		}
	}
}

func (c *client) ensureConnected(ctx context.Context) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if connected {
		return nil
	}

	if c.cfg.DisableAutoReconnect {
		return fmt.Errorf("not connected to server")
	}

	return c.reconnect(ctx)
}

func (c *client) reconnect(ctx context.Context) error {
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if connected {
		return nil
	}

	return c.ConnectWithRetry(ctx)
}

func (c *client) markDisconnected(err error) {
	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return
	}
	c.connected = false
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.qconn = nil
	c.mu.Unlock()

	if err != nil {
		c.log.Warn().Err(err).Msg("Connection lost")
	}

	if !c.cfg.DisableAutoReconnect {
		c.startReconnectLoop()
	}
}

func (c *client) startReconnectLoop() {
	c.reconnMu.Lock()
	if c.reconnecting {
		c.reconnMu.Unlock()
		return
	}
	c.reconnecting = true
	c.reconnMu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			c.reconnMu.Lock()
			c.reconnecting = false
			c.reconnMu.Unlock()
		}()

		for {
			select {
			case <-c.stopCh:
				return
			default:
			}

			if c.IsConnected() {
				return
			}

			if err := c.ConnectWithRetry(context.Background()); err != nil {
				c.log.Warn().Err(err).Msg("Reconnect attempt failed")
				select {
				case <-c.stopCh:
					return
				case <-time.After(c.cfg.ReconnectDelay):
				}
				continue
			}
			return
		}
	}()
}

func (c *client) shouldReconnect(err error) bool {
	if err == nil {
		return false
	}
	var (
		idleTimeout      *quic.IdleTimeoutError
		handshakeTimeout *quic.HandshakeTimeoutError
		statelessReset   *quic.StatelessResetError
		transportErr     *quic.TransportError
		appErr           *quic.ApplicationError
		versionErr       *quic.VersionNegotiationError
	)
	return errors.As(err, &idleTimeout) ||
		errors.As(err, &handshakeTimeout) ||
		errors.As(err, &statelessReset) ||
		errors.As(err, &transportErr) ||
		errors.As(err, &appErr) ||
		errors.As(err, &versionErr)
}

func clientKeepAlive(cfg ClientConfig) time.Duration {
	if cfg.DisableKeepAlive {
		return 0
	}
	if cfg.KeepAlivePeriod == 0 {
		return defaultKeepAlivePeriod
	}
	return cfg.KeepAlivePeriod
}
