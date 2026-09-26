// Package irohacp carries the Agent Client Protocol over iroh. Each ACP
// connection is one bidirectional QUIC stream between two iroh endpoints,
// framed exactly like ACP over stdio (newline-delimited JSON-RPC), so any
// stdio agent or client works unchanged at either end. Peers find each other
// with endpoint tickets and connect directly or through the n0 relays.
package irohacp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/iroh"
	"github.com/tmc/go-iroh/key"
	"github.com/tmc/go-iroh/relay"
)

// ALPN identifies ACP streams on an iroh connection.
const ALPN = "acp/1"

// Bind binds an endpoint that accepts ALPN and uses the n0 relays; opts
// override those defaults. Its connections close after 10s without word from
// the peer (iroh's default is 30s), so an agent whose client was killed, as
// editors stop agents, does not outlive it by long.
func Bind(ctx context.Context, opts ...iroh.Option) (*iroh.Endpoint, error) {
	return iroh.Bind(ctx, append([]iroh.Option{
		iroh.WithALPNs(ALPN),
		iroh.WithRelayMode(relay.ModeDefault()),
		iroh.WithTransportConfig(&iroh.QUICTransportConfig{MaxIdleTimeout: 10 * time.Second}),
	}, opts...)...)
}

// LoadKey returns the secret key saved at path, creating it first if needed,
// so an endpoint that uses it keeps its ID, and a server its ticket, across
// restarts.
func LoadKey(path string) (key.SecretKey, error) {
	seed, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		seed = make([]byte, key.SeedSize)
		_, _ = rand.Read(seed)
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			err = os.WriteFile(path, seed, 0o600)
		}
	}
	if err != nil {
		return key.SecretKey{}, err
	}
	return key.SecretKeyFromSlice(seed)
}

// Dial opens an ACP stream to the endpoint in ticket. Its CloseWrite closes
// only the send side, like closing an agent's stdin.
func Dial(ctx context.Context, ep *iroh.Endpoint, ticket string) (net.Conn, error) {
	addr, err := endpointticket.Decode(ticket)
	if err != nil {
		return nil, err
	}
	return ep.Dial(ctx, addr, ALPN)
}

// AllowIDs returns an allow func for [Serve] that accepts only ids.
func AllowIDs(ids ...key.EndpointID) func(key.EndpointID) bool {
	return func(id key.EndpointID) bool { return slices.Contains(ids, id) }
}

// Serve calls handle in a new goroutine for each ACP stream ep accepts, and
// closes the stream once handle returns. It disconnects peers whose ID allow
// rejects, with the reason "not allowed", and logs them. It runs until ctx is
// done.
func Serve(ctx context.Context, ep *iroh.Endpoint, allow func(key.EndpointID) bool, handle func(net.Conn)) error {
	l := iroh.NewStreamListener()
	// Unlike ep.ListenStreams, a Router keeps accepting after a rejection:
	// https://github.com/tmc/go-iroh/issues/25
	_, err := iroh.NewRouter(ep, map[string]iroh.ProtocolHandler{ALPN: iroh.ProtocolHandlerFunc(func(rctx context.Context, c *iroh.Conn) error {
		if !allow(c.RemoteID()) {
			_ = c.CloseWithError(1, "not allowed")
			return fmt.Errorf("rejected %s", c.RemoteID())
		}
		return l.Handler().Accept(rctx, c)
	})}, nil)
	if err != nil {
		return err
	}
	defer context.AfterFunc(ctx, func() { l.Close() })()
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer c.Close()
			handle(c)
		}()
	}
}

// ServeAgent serves a new acp-go agent on each ACP stream ep accepts from a
// peer that allow accepts.
func ServeAgent(ctx context.Context, ep *iroh.Endpoint, allow func(key.EndpointID) bool, newAgent func(*acp1.AgentSideConnection) acp1.Agent) error {
	return Serve(ctx, ep, allow, func(c net.Conn) {
		_ = acp1.NewAgentSideConnection(newAgent, acp.NewStdioTransport(c, c)).Start(ctx)
	})
}

// ConnectAgent connects an acp-go client to the agent served at ticket.
func ConnectAgent(ctx context.Context, ep *iroh.Endpoint, ticket string, newClient func(*acp1.ClientSideConnection) acp1.Client) (*acp1.RemoteAgent, error) {
	c, err := Dial(ctx, ep, ticket)
	if err != nil {
		return nil, err
	}
	agent := acp1.ConnectAgent(ctx, acp.NewStdioTransport(c, c), newClient)
	go func() {
		// acp-go closes the transport only after its read unblocks:
		// https://github.com/ironpark/acp-go/issues/11
		<-agent.Done()
		_ = c.Close()
	}()
	return agent, nil
}
