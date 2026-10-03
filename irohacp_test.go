package irohacp_test

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

// TestServeRejectsQueuedStream checks that a peer allow rejects gets no stream,
// even one that arrived before allow returned.
func TestServeRejectsQueuedStream(t *testing.T) {
	opts := []iroh.Option{iroh.WithBindAddr(netip.MustParseAddrPort("127.0.0.1:0")), iroh.WithRelayMode(relay.ModeDisabled())}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	server := bind(t, opts...)
	handled := make(chan struct{}, 1)
	slowReject := func(key.EndpointID) bool {
		time.Sleep(300 * time.Millisecond) // the stranger's stream arrives meanwhile
		return false
	}
	go func() { _ = irohacp.Serve(ctx, server, slowReject, func(net.Conn) { handled <- struct{}{} }) }()
	c, err := irohacp.Dial(ctx, bind(t, opts...), endpointticket.Encode(server.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Error("read from a rejected stream")
	}
	select {
	case <-handled:
		t.Error("stranger's stream was served")
	case <-time.After(time.Second):
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
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("key file: %v %v", info, err)
	}
}

// startServer runs acp-server with args until it is killed or the test ends.
// It returns the server, its ticket, and a channel that receives each time one
// of its agents exits.
func startServer(t *testing.T, bin string, args ...string) (*exec.Cmd, string, <-chan struct{}) {
	t.Helper()
	server := exec.CommandContext(t.Context(), filepath.Join(bin, "acp-server"), args...)
	stderr, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	server.Stderr = w
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	t.Cleanup(func() { _ = server.Wait() })
	exited := make(chan struct{}, 10)
	go func() {
		for lines := bufio.NewScanner(stderr); lines.Scan(); {
			fmt.Fprintln(os.Stderr, lines.Text())
			if strings.Contains(lines.Text(), "agent exited") {
				exited <- struct{}{}
			}
		}
	}()
	ticket, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return server, strings.TrimSpace(ticket), exited
}

// TestBinaries runs acp-go's echo agent behind acp-server and drives
// acp-client the way an editor would: as a stdio agent subprocess.
func TestBinaries(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and waits for the n0 relays")
	}
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", bin, "./cmd/acp-server", "./cmd/acp-client", "github.com/ironpark/acp-go/examples/echo")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	keyFlag := func(name string) string { return "-key=" + filepath.Join(bin, name) }
	id, err := exec.Command(filepath.Join(bin, "acp-client"), keyFlag("client.key")).Output()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{keyFlag("server.key"), "-allow=" + strings.TrimSpace(string(id)), filepath.Join(bin, "echo")}
	server, ticket, exited := startServer(t, bin, args...)
	connect := func(key string, stderr io.Writer) (*acp1.RemoteAgent, *exec.Cmd) {
		t.Helper()
		client := exec.Command(filepath.Join(bin, "acp-client"), keyFlag(key), ticket)
		client.Stderr = stderr
		agent, err := acp1.SpawnAgent(t.Context(), client, unimplementedClient)
		if err != nil {
			t.Fatal(err)
		}
		return agent, client
	}
	agentExits := func(within time.Duration) {
		t.Helper()
		select {
		case <-exited:
		case <-time.After(within):
			t.Fatalf("remote agent still running after %v", within)
		}
	}

	for _, want := range []string{"hello from acp-client", "the server keeps serving"} {
		agent, _ := connect("client.key", os.Stderr)
		if got := prompt(t, agent, want); got != want {
			t.Errorf("echo = %q, want %q", got, want)
		}
		// Closing stdin must end the remote agent and then acp-client, well
		// before acp-go would kill it.
		start := time.Now()
		if err := agent.Close(); err != nil {
			t.Fatal(err)
		}
		if err := agent.Wait(); err != nil || time.Since(start) > acp1.ExitGrace/2 {
			t.Fatalf("acp-client exited after %v: %v", time.Since(start), err)
		}
		agentExits(time.Second)
	}

	t.Run("client not allowed", func(t *testing.T) {
		var stderr strings.Builder
		stranger, _ := connect("stranger.key", &stderr)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if _, err := stranger.Initialize(ctx, &acp1.InitializeRequest{}); err == nil {
			t.Error("stranger was served")
		}
		_ = stranger.Close()
		if err := stranger.Wait(); err == nil || !strings.Contains(stderr.String(), "not allowed") {
			t.Errorf("acp-client exited with %v, stderr %q", err, stderr.String())
		}
	})

	t.Run("client killed", func(t *testing.T) {
		// Zed stops agents with SIGKILL, so acp-client cannot say goodbye.
		agent, client := connect("client.key", os.Stderr)
		if got := prompt(t, agent, "about to be killed"); got != "about to be killed" {
			t.Errorf("echo = %q", got)
		}
		start := time.Now()
		_ = client.Process.Kill()
		agentExits(15 * time.Second)
		t.Logf("remote agent exited %v after its client was killed", time.Since(start).Round(time.Second))
		_ = agent.Close()
	})

	t.Run("server restarted", func(t *testing.T) {
		_ = server.Process.Kill()
		_, _, exited = startServer(t, bin, args...)
		agent, _ := connect("client.key", os.Stderr) // with the old ticket
		if got := prompt(t, agent, "same ticket"); got != "same ticket" {
			t.Errorf("echo = %q", got)
		}
		_ = agent.Close()
		_ = agent.Wait()
		agentExits(time.Second)
	})
}
