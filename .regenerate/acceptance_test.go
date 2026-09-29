// Package regenerate checks an implementation of iroh-acp-go against SPEC.md.
// The tests drive only the exported API, the built binaries and go-iroh
// itself, and compare the exported declarations and the flags with the spec,
// so they can judge the code in this repository or a rebuild of it.
package regenerate

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/acp-go/acp1"
	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/iroh"
	"github.com/tmc/go-iroh/key"
	"github.com/tmc/go-iroh/relay"

	irohacp "github.com/carsonfarmer/iroh-acp-go"
)

// echoAgent streams each prompt back to the client.
type echoAgent struct{ client acp1.Client }

func (a *echoAgent) Initialize(context.Context, *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	return &acp1.InitializeResponse{ProtocolVersion: acp1.ProtocolVersion}, nil
}

func (a *echoAgent) NewSession(context.Context, *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	return &acp1.NewSessionResponse{SessionID: acp1.GenerateSessionID()}, nil
}

func (a *echoAgent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	stream := acp1.NewSessionStream(a.client, params.SessionID)
	for text := range acp1.Texts(params.Prompt) {
		if err := stream.SendText(ctx, text); err != nil {
			return nil, err
		}
	}
	return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
}

func (a *echoAgent) CancelSession(context.Context, *acp1.CancelNotification) error { return nil }

func bind(t *testing.T, opts ...iroh.Option) *iroh.Endpoint {
	t.Helper()
	ep, err := irohacp.Bind(t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ep.Shutdown(context.Background()) })
	return ep
}

// prompt runs one turn on agent and returns the text it streamed back.
func prompt(t *testing.T, agent *acp1.RemoteAgent, text string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := agent.Initialize(ctx, &acp1.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	session, err := agent.StartSession(ctx, &acp1.NewSessionRequest{Cwd: "/"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := session.Prompt(ctx, acp1.TextBlock(text))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := turn.Text()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func unimplementedClient(*acp1.ClientSideConnection) acp1.Client { return acp1.UnimplementedClient{} }

func TestServeAgentConnectAgent(t *testing.T) {
	loopback := iroh.WithBindAddr(netip.MustParseAddrPort("127.0.0.1:0"))
	tests := []struct {
		name    string
		opts    []iroh.Option
		network bool
	}{
		{name: "direct", opts: []iroh.Option{loopback, iroh.WithRelayMode(relay.ModeDisabled())}},
		{name: "relay only", opts: []iroh.Option{iroh.WithoutIPTransports()}, network: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.network && testing.Short() {
				t.Skip("needs the n0 relays")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			server := bind(t, tt.opts...)
			if tt.network {
				if err := server.Online(ctx); err != nil {
					t.Fatal(err)
				}
			}
			clients := []*iroh.Endpoint{bind(t, tt.opts...), bind(t, tt.opts...)}
			go func() {
				_ = irohacp.ServeAgent(ctx, server, irohacp.AllowIDs(clients[0].ID(), clients[1].ID()), func(c *acp1.AgentSideConnection) acp1.Agent { return &echoAgent{client: c} })
			}()
			ticket := endpointticket.Encode(server.Addr())
			// A peer that is not allowed gets no agent, and the server carries on.
			if stranger, err := irohacp.ConnectAgent(ctx, bind(t, tt.opts...), ticket, unimplementedClient); err == nil {
				if _, err := stranger.Initialize(ctx, &acp1.InitializeRequest{}); err == nil {
					t.Error("stranger was served")
				}
				_ = stranger.Close()
			}
			// Two connections at once: each gets its own agent.
			for i, want := range []string{"hello over iroh", "second connection"} {
				agent, err := irohacp.ConnectAgent(ctx, clients[i], ticket, unimplementedClient)
				if err != nil {
					t.Fatal(err)
				}
				defer agent.Close()
				if got := prompt(t, agent, want); got != want {
					t.Errorf("echo = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestLoadKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iroh-acp", "test.key")
	created, err := irohacp.LoadKey(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := irohacp.LoadKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Public() != created.Public() {
		t.Errorf("reloaded key %v, want %v", loaded.Public(), created.Public())
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Errorf("key file: %v %v", info, err)
	}
}

// echo writes bytes that are not ACP to c, closes its write side, and checks
// that the same bytes come back, followed by EOF.
func echo(t *testing.T, c net.Conn) {
	t.Helper()
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	const want = "{\"jsonrpc\":\"2.0\"}\r\n\n\x00\xff no newline at the end"
	if _, err := io.WriteString(c, want); err != nil {
		t.Fatal(err)
	}
	cw, ok := c.(interface{ CloseWrite() error })
	if !ok {
		t.Fatalf("%T has no CloseWrite", c)
	}
	if err := cw.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(c); string(got) != want || err != nil {
		t.Fatalf("echo = %q, %v; want %q", got, err, want)
	}
}

// TestWire checks the wire behavior in section 4 of SPEC.md. Plain go-iroh
// stands in for another implementation at one end, so the ALPN, the bytes and
// the close code must be the ones the spec names, not just ones this
// implementation agrees with itself on.
func TestWire(t *testing.T) {
	opts := []iroh.Option{iroh.WithBindAddr(netip.MustParseAddrPort("127.0.0.1:0")), iroh.WithRelayMode(relay.ModeDisabled())}
	plain := func(t *testing.T, extra ...iroh.Option) *iroh.Endpoint {
		t.Helper()
		ep, err := iroh.Bind(t.Context(), slices.Concat(opts, extra)...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ep.Shutdown(context.Background()) })
		return ep
	}
	serveEcho := func(t *testing.T, ep *iroh.Endpoint, allow func(key.EndpointID) bool) {
		t.Helper()
		go func() {
			_ = irohacp.Serve(t.Context(), ep, allow, func(c net.Conn) { _, _ = io.Copy(c, c) })
		}()
	}

	t.Run("Dial to Serve", func(t *testing.T) {
		server, client := bind(t, opts...), bind(t, opts...)
		serveEcho(t, server, irohacp.AllowIDs(client.ID()))
		c, err := irohacp.Dial(t.Context(), client, endpointticket.Encode(server.Addr()))
		if err != nil {
			t.Fatal(err)
		}
		echo(t, c)
	})

	t.Run("go-iroh to Serve", func(t *testing.T) {
		server, client := bind(t, opts...), plain(t)
		serveEcho(t, server, irohacp.AllowIDs(client.ID()))
		c, err := client.Dial(t.Context(), server.Addr(), "acp/1")
		if err != nil {
			t.Fatal(err)
		}
		echo(t, c)
	})

	t.Run("Dial to go-iroh", func(t *testing.T) {
		server, client := plain(t, iroh.WithALPNs("acp/1")), bind(t, opts...)
		accepted := make(chan *iroh.Conn, 1)
		go func() {
			conn, err := server.Accept(t.Context())
			if err != nil {
				return
			}
			accepted <- conn
			if s, err := conn.AcceptStreamConn(t.Context()); err == nil {
				_, _ = io.Copy(s, s)
				_ = s.Close()
			}
		}()
		c, err := irohacp.Dial(t.Context(), client, endpointticket.Encode(server.Addr()))
		if err != nil {
			t.Fatal(err)
		}
		echo(t, c)
		if conn := <-accepted; conn.ALPN() != "acp/1" || conn.RemoteID() != client.ID() {
			t.Errorf("go-iroh accepted ALPN %q from %v, want %q from %v", conn.ALPN(), conn.RemoteID(), "acp/1", client.ID())
		}
	})

	t.Run("go-iroh rejected by Serve", func(t *testing.T) {
		server, stranger := bind(t, opts...), plain(t)
		serveEcho(t, server, irohacp.AllowIDs())
		c, err := stranger.Dial(t.Context(), server.Addr(), "acp/1")
		if err == nil {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(30 * time.Second))
			_, _ = io.WriteString(c, "hello")
			_, err = io.ReadAll(c)
		}
		if closed, ok := iroh.AsApplicationError(err); !ok || closed.Code != 1 || closed.Reason != "not allowed" || !closed.Remote {
			t.Errorf("stranger got %v, want the server to close with code 1 and reason %q", err, "not allowed")
		}
	})
}

// startServer runs acp-server with args until it is killed or the test ends.
// It returns the server, its ticket, and the lines the server writes to
// stderr. When the server exits, it checks that the ticket was all the server
// wrote to stdout.
func startServer(t *testing.T, bin string, args ...string) (*exec.Cmd, string, <-chan string) {
	t.Helper()
	server := exec.CommandContext(t.Context(), filepath.Join(bin, "acp-server"), args...)
	stdout, outw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, errw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	server.Stdout, server.Stderr = outw, errw
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	_, _ = outw.Close(), errw.Close()
	lines := make(chan string, 100)
	go func() {
		for s := bufio.NewScanner(stderr); s.Scan(); {
			fmt.Fprintln(os.Stderr, s.Text())
			lines <- s.Text()
		}
	}()
	out := bufio.NewReader(stdout)
	ticket, err := out.ReadString('\n')
	if err != nil || !regexp.MustCompile(`^endpoint[a-z2-7]+\n$`).MatchString(ticket) {
		t.Fatalf("acp-server printed %q, want a ticket: %v", ticket, err)
	}
	t.Cleanup(func() {
		_ = server.Wait()
		if rest, _ := io.ReadAll(out); len(rest) > 0 {
			t.Errorf("acp-server printed %q after its ticket", rest)
		}
	})
	return server, strings.TrimSpace(ticket), lines
}

// waitFor reads lines until one contains text.
func waitFor(t *testing.T, lines <-chan string, text string, within time.Duration) {
	t.Helper()
	timeout := time.After(within)
	for {
		select {
		case line := <-lines:
			if strings.Contains(line, text) {
				return
			}
		case <-timeout:
			t.Fatalf("acp-server did not log %q within %v", text, within)
		}
	}
}

// build builds pkgs into a new temporary directory and returns it.
func build(t *testing.T, pkgs ...string) string {
	t.Helper()
	bin := t.TempDir()
	if out, err := exec.Command("go", append([]string{"build", "-o", bin}, pkgs...)...).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return bin
}

// TestBinaries runs acp-go's echo agent behind acp-server and drives
// acp-client the way an editor would: as a stdio agent subprocess.
func TestBinaries(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and waits for the n0 relays")
	}
	bin := build(t, "github.com/carsonfarmer/iroh-acp-go/cmd/...", "github.com/ironpark/acp-go/examples/echo")
	keyFlag := func(name string) string { return "-key=" + filepath.Join(bin, name) }
	clientID := func(name string) string {
		out, err := exec.Command(filepath.Join(bin, "acp-client"), keyFlag(name)).Output()
		if err != nil || !regexp.MustCompile(`^[0-9a-f]{64}\n$`).Match(out) {
			t.Fatalf("acp-client printed %q, want an ID: %v", out, err)
		}
		return strings.TrimSpace(string(out))
	}
	alice, bob, strangerID := "-allow="+clientID("alice.key"), "-allow="+clientID("bob.key"), clientID("stranger.key")

	for _, bad := range [][]string{{keyFlag("usage.key"), "sh"}, {keyFlag("usage.key"), alice}} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		out, err := exec.CommandContext(ctx, filepath.Join(bin, "acp-server"), bad...).CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(out), "usage: acp-server") {
			t.Errorf("acp-server %v: %v, %q; want the usage error", bad, err, out)
		}
	}

	// The agent writes to stderr before it starts, which the server must pass
	// on. The server must also run the command with its arguments as given.
	args := []string{keyFlag("server.key"), alice, bob, "sh", "-c", `echo hello from the agent >&2; exec "$0"`, filepath.Join(bin, "echo")}

	server, ticket, lines := startServer(t, bin, args...)
	connect := func(t *testing.T, key string, stderr io.Writer) (*acp1.RemoteAgent, *exec.Cmd) {
		t.Helper()
		client := exec.Command(filepath.Join(bin, "acp-client"), keyFlag(key), ticket)
		client.Stderr = stderr
		agent, err := acp1.SpawnAgent(t.Context(), client, unimplementedClient)
		if err != nil {
			t.Fatal(err)
		}
		return agent, client
	}

	for _, key := range []string{"alice.key", "bob.key"} {
		want := "hello from " + key
		agent, _ := connect(t, key, os.Stderr)
		if got := prompt(t, agent, want); got != want {
			t.Errorf("echo = %q, want %q", got, want)
		}
		waitFor(t, lines, "hello from the agent", time.Second)
		// Closing stdin must end the remote agent and then acp-client, well
		// before acp-go would kill it.
		start := time.Now()
		if err := agent.Close(); err != nil {
			t.Fatal(err)
		}
		if err := agent.Wait(); err != nil || time.Since(start) > acp1.ExitGrace/2 {
			t.Fatalf("acp-client exited after %v: %v", time.Since(start), err)
		}
		waitFor(t, lines, "agent exited", time.Second)
	}

	t.Run("client not allowed", func(t *testing.T) {
		var stderr strings.Builder
		agent, _ := connect(t, "stranger.key", &stderr)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if _, err := agent.Initialize(ctx, &acp1.InitializeRequest{}); err == nil {
			t.Error("stranger was served")
		}
		_ = agent.Close()
		if err := agent.Wait(); err == nil || !strings.Contains(stderr.String(), "not allowed") {
			t.Errorf("acp-client exited with %v, stderr %q", err, stderr.String())
		}
		waitFor(t, lines, "rejected "+strangerID, 10*time.Second)
	})

	t.Run("client killed", func(t *testing.T) {
		// Zed stops agents with SIGKILL, so acp-client cannot say goodbye.
		agent, client := connect(t, "alice.key", os.Stderr)
		if got := prompt(t, agent, "about to be killed"); got != "about to be killed" {
			t.Errorf("echo = %q", got)
		}
		start := time.Now()
		_ = client.Process.Kill()
		waitFor(t, lines, "agent exited", 15*time.Second)
		t.Logf("remote agent exited %v after its client was killed", time.Since(start).Round(time.Second))
		_ = agent.Close()
	})

	t.Run("server restarted", func(t *testing.T) {
		_ = server.Process.Kill()
		_, _, lines := startServer(t, bin, args...)
		agent, _ := connect(t, "alice.key", os.Stderr) // with the old ticket
		if got := prompt(t, agent, "same ticket"); got != "same ticket" {
			t.Errorf("echo = %q", got)
		}
		_ = agent.Close()
		_ = agent.Wait()
		waitFor(t, lines, "agent exited", time.Second)
	})
}
