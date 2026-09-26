# iroh-acp-go

[![Test](https://github.com/carsonfarmer/iroh-acp-go/actions/workflows/test.yml/badge.svg)](https://github.com/carsonfarmer/iroh-acp-go/actions/workflows/test.yml)
[![Lint](https://github.com/carsonfarmer/iroh-acp-go/actions/workflows/lint.yml/badge.svg)](https://github.com/carsonfarmer/iroh-acp-go/actions/workflows/lint.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/carsonfarmer/iroh-acp-go.svg)](https://pkg.go.dev/github.com/carsonfarmer/iroh-acp-go)

Run an [Agent Client Protocol](https://agentclientprotocol.com) (ACP) agent on one
machine and use it from an editor on another, peer to peer over
[iroh](https://github.com/tmc/go-iroh). There are no open ports, no VPN and no server in
the middle.

```
editor ──stdio── acp-client ══ iroh (QUIC, direct or relayed) ══ acp-server ──stdio── agent
```

It is two small binaries and a Go library, built almost entirely from
[go-iroh](https://github.com/tmc/go-iroh) and [acp-go](https://github.com/ironpark/acp-go).
The library is under 100 lines of code, and each binary is under 50.

## Why

Editors such as Zed start ACP agents as local subprocesses and talk to them over stdio.
That's fine until the agent should run somewhere else: on a big dev box, next to a
repository, or on a machine that holds the credentials. The usual answers are SSH
tunnels, port forwarding or a VPN.

iroh gives each process a public-key identity and connects two of them from anywhere.
It punches through NATs where it can and falls back to relays where it can't, and the
connection is always end-to-end encrypted. This project carries ACP over that
connection, so the editor still just launches a local command: `acp-client`.

## How it works

- **Identity.** Each endpoint has an Ed25519 key, and its public half is the endpoint
  ID. Both binaries save their key to a file, so IDs survive restarts.
- **Addressing.** `acp-server` prints an *endpoint ticket*. The ticket holds the
  server's ID, its home relay and its current direct addresses. Because the ID is
  stable, a ticket keeps working after the server restarts.
- **Connectivity.** `acp-client` dials the ticket and iroh picks the path. It connects
  directly when it can and goes through the [n0](https://n0.computer) public relays
  when it can't. Relays forward only encrypted QUIC packets.
- **Access control.** The QUIC handshake proves the client's ID. The server checks
  that ID against its allowlist before it starts anything, and disconnects other peers
  with "not allowed".
- **Framing.** Each ACP connection is one bidirectional QUIC stream, under the ALPN
  `acp/1`. The stream carries ACP's usual newline-delimited JSON-RPC, byte for byte.
  So both binaries are plain pipes, and any stdio agent and any ACP client work
  unchanged.
- **Lifecycle.** The server runs one agent process per connection:
  - When the editor closes the agent's stdin, `acp-client` half-closes the stream. The
    agent sees EOF and exits. The server then closes the stream, and `acp-client` sees
    EOF and exits.
  - If the editor kills `acp-client` outright, as Zed does, the connection times out
    after 10s without word from the client, and the agent's stdin closes.
  - iroh sends keepalives every 5s, so a quiet but live session stays up.

## Install

Needs Go 1.27. With `GOTOOLCHAIN=auto`, the default, Go fetches it on demand.

```bash
go install github.com/carsonfarmer/iroh-acp-go/cmd/...@latest
```

This installs `acp-server` and `acp-client`.

## Quick start

To try it without a real agent, use acp-go's example agent and interactive client:

```bash
go build -o bin/ ./cmd/... github.com/ironpark/acp-go/examples/agent github.com/ironpark/acp-go/examples/client
```

1. On the machine with the editor, print the client's ID:

   ```bash
   bin/acp-client
   ```

2. On the machine with the agent, start the server and allow that ID. It prints a
   ticket:

   ```bash
   bin/acp-server -allow <client-id> bin/agent
   ```

3. Back on the editor's machine, connect with the example client. It spawns
   `acp-client <ticket>` as if that were the agent:

   ```bash
   bin/client bin/acp-client <ticket>
   ```

Try `refactor the parser`. The remote agent does four things, and every one of those
calls crosses iroh:

- streams a plan,
- runs `go version` in a terminal on *your* machine,
- reads files through your client,
- asks your permission before a change.

## Use a real agent from Zed

On the agent's machine:

```bash
acp-server -allow <client-id> npx -y @zed-industries/claude-code-acp
```

On the editor's machine, add this to Zed's `settings.json`, then pick "Remote Claude"
in the agent panel:

```json
{
  "agent_servers": {
    "Remote Claude": {
      "type": "custom",
      "command": "/absolute/path/to/acp-client",
      "args": ["<ticket>"]
    }
  }
}
```

Any other ACP client that launches agents from a command and arguments is set up the
same way.

The agent runs on the server's machine, so it reads and edits that machine's files.
ACP clients that serve `fs/*` and `terminal/*` requests, like acp-go's example client,
handle those on the editor's side.

## Commands

### acp-server

```
acp-server [-key file] -allow <client-id> [-allow <client-id> ...] <agent-command> [args...]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-allow` | required | A client ID to serve, as printed by `acp-client`. Repeat it to serve several clients. |
| `-key` | `<config dir>/iroh-acp/server.key` | The server's key file. It is created if it's missing. |

The server prints its ticket on stdout and logs to stderr, including the ID of each
client it rejects. The agent's stderr goes to the server's stderr.

### acp-client

```
acp-client [-key file]            # print this client's ID
acp-client [-key file] <ticket>   # bridge stdio to the agent at ticket
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-key` | `<config dir>/iroh-acp/client.key` | The client's key file. It is created if it's missing. |

`<config dir>` is Go's [`os.UserConfigDir`](https://pkg.go.dev/os#UserConfigDir):

- `~/Library/Application Support` on macOS,
- `$XDG_CONFIG_HOME` or `~/.config` on Linux,
- `%AppData%` on Windows.

Give each machine its own client key.

## Library

`github.com/carsonfarmer/iroh-acp-go` (package `irohacp`) gives acp-go agents and
clients the same transport, without subprocesses:

```go
// Serve an agent to one client.
sk, _ := irohacp.LoadKey("server.key")
ep, _ := irohacp.Bind(ctx, iroh.WithSecretKey(sk))
fmt.Println(endpointticket.Encode(ep.Addr()))
go irohacp.ServeAgent(ctx, ep, irohacp.AllowIDs(clientID), func(c *acp1.AgentSideConnection) acp1.Agent {
	return &myAgent{client: c}
})

// Elsewhere: connect to it.
agent, _ := irohacp.ConnectAgent(ctx, clientEp, ticket, newClient)
defer agent.Close()
```

| Function | What it does |
| --- | --- |
| `Bind(ctx, opts...)` | Binds an iroh endpoint for ACP: ALPN `acp/1`, the n0 relays, a 10s idle timeout. `opts` override these, for example to use your own relays. |
| `LoadKey(path)` | Loads a secret key, creating and saving one on first use. |
| `Dial(ctx, ep, ticket)` | Opens an ACP stream to a ticket, as a `net.Conn` with `CloseWrite`. |
| `Serve(ctx, ep, allow, handle)` | Calls `handle` with each ACP stream from a peer that `allow` accepts. |
| `AllowIDs(ids...)` | The usual `allow`: accept exactly these endpoint IDs. |
| `ServeAgent(ctx, ep, allow, newAgent)` | `Serve` with a new acp-go agent per stream. |
| `ConnectAgent(ctx, ep, ticket, newClient)` | `Dial` plus an acp-go client connection. |

`allow` is a `func(key.EndpointID) bool`, so any other access policy is one function
away.

## Security model

- **Only allowed clients get an agent.** The ID a client presents is authenticated by
  the QUIC TLS handshake, so it cannot be spoofed without the client's key.
- **Key files are credentials.** They are written with mode `0600`. Anyone holding a
  client's key can act as that client, so keep them private.
- **An allowed client can do whatever the agent can** on the server's machine. Coding
  agents run commands and edit files, so allow only clients you trust with that
  machine.
- **The ticket is an address, not a secret.** It grants nothing on its own, but it does
  contain the server's IP addresses.
- **Traffic is end-to-end encrypted.** Relays see which endpoints talk to each other,
  but not what they say.

## Development

```bash
go test -race ./...     # everything, including relay-only and built-binary tests
go test -short ./...    # loopback only, no network
golangci-lint run       # the configuration is in .golangci.yml
```

Tests never touch your real key files.

- `TestServeAgentConnectAgent` runs the library over direct loopback and over the n0
  relays only. It checks two concurrent agents and a rejected stranger.
- `TestBinaries` builds `acp-server`, `acp-client` and acp-go's echo agent, then drives
  `acp-client` the way an editor does. It covers:
  - clean shutdown,
  - a client that isn't allowed,
  - a client killed with SIGKILL, whose agent must exit within about 10s,
  - a server restart that keeps the old ticket working.

CI runs these tests, golangci-lint and govulncheck on every push and pull request.
Dependabot keeps Go modules and actions up to date.

## Caveats and known issues

- A client that is killed leaves its agent running for up to 10s, until the connection
  times out.
- By default, peers that can't connect directly use n0's public relays. For production,
  pass your own relay configuration to `Bind`.
- acp-go's `RemoteAgent.Close` can hang when it runs over a stdio transport on a
  socket. `ConnectAgent` works around this by closing the stream itself.
- In go-iroh, `Endpoint.ListenStreams` stops accepting after a handshake hook rejects
  a peer. `Serve` uses an `iroh.Router` instead, which keeps going.

## Acknowledgements

- [go-iroh](https://github.com/tmc/go-iroh) by Travis Cline, a Go port of
  [iroh](https://github.com/n0-computer/iroh) by n0.
- [acp-go](https://github.com/ironpark/acp-go) by ironpark.
- The [Agent Client Protocol](https://agentclientprotocol.com) by Zed Industries.

## License

[MIT](LICENSE)
