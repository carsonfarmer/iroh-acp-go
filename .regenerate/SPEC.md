# iroh-acp-go specification

This file states what iroh-acp-go must do. It was reconstructed from the code at
commit 0fbc125, the README and the tests, so it describes the reference
implementation as it is. MUST, SHOULD and MAY are used as in RFC 2119.

A blind rebuild (see `PROMPT.md`) gets this file and nothing from the reference
source. If a rebuild fails the tests because this file was silent or vague, fix the
file and run the rebuild again.

## 1. Purpose

Carry the Agent Client Protocol (ACP) over iroh, so an ACP client on one machine can
use an ACP agent on another with no open ports and no server in between.

ACP over stdio is newline-delimited JSON-RPC. iroh is a QUIC peer-to-peer transport
where each endpoint is an Ed25519 key pair and its public key is its address. This
project maps one to the other:

- one ACP connection is one bidirectional QUIC stream, and the bytes on it are the
  bytes ACP would carry on stdio, unchanged;
- the server allows only the client IDs it was told about;
- both binaries keep their keys in files, so IDs and tickets survive restarts.

The deliverable is a Go module with a library and two commands.

## 2. Inputs that are fixed

| Item | Value |
| --- | --- |
| Module path | `github.com/carsonfarmer/iroh-acp-go` |
| Go version and dependencies | As pinned in `go.mod` and `go.sum`. Do not change the `go` line or the direct requirements. `GOTOOLCHAIN=auto` fetches the toolchain. |
| iroh in Go | `github.com/tmc/go-iroh` |
| ACP in Go | `github.com/ironpark/acp-go` |
| Layout | `irohacp.go` (package `irohacp`), `cmd/acp-server/main.go`, `cmd/acp-client/main.go` |
| Size goal | fewest custom lines. Library under 100 non-comment, non-blank lines; each command under 50 |
| License | MIT (unchanged) |

Everything else comes from those two dependencies and the standard library. The
implementation MUST NOT copy code out of either dependency's module cache into the
repository. It MUST NOT add other direct dependencies.

Read the dependencies' source before using them. Their exported names are the
truth, and this file does not restate them.

## 3. Definitions

- **Endpoint ID.** The endpoint's Ed25519 public key. Printed as 64 lowercase
  hexadecimal characters. `key.ParseEndpointID` reads that form.
- **Ticket.** The string produced by `endpointticket.Encode(ep.Addr())`. It is one
  line of lowercase text that starts with `endpoint`. It carries the server's ID,
  its home relay and its current direct addresses. It grants nothing by itself.
- **Client, server, agent.** The client is the ACP client process on the user's
  side (an editor). `acp-client` stands in for the agent on that side. `acp-server`
  runs the real agent as a child process on the other side.
- **Key file.** A file that holds exactly the 32 raw bytes of an Ed25519 seed. No
  header, no encoding.

## 4. Wire behavior

1. The ALPN is `acp/1`. It is the exported constant `ALPN`.
2. The dialer opens one bidirectional stream. The bytes on that stream are ACP's
   newline-delimited JSON-RPC in both directions. Neither side adds, removes,
   buffers to a boundary, or rewrites bytes.
3. The dialer closing its write side (`CloseWrite`) means "stdin EOF" to the agent.
   It MUST NOT close the read side.
4. A server accepts a connection only from a peer whose endpoint ID its allow
   function accepts. The ID is authenticated by the QUIC TLS handshake. For any other
   peer, the server closes the connection with application error code `1` and the
   reason `not allowed`. It does not start an agent, even for a stream that arrived
   before the allow function returned.
5. A rejected peer MUST NOT stop the server from accepting other peers.
6. Connections use a maximum idle timeout of 10 seconds. iroh's default is 30. go-iroh
   sends keepalives every 5 seconds, so a quiet, live connection stays open. QUIC
   restarts the idle timer when an endpoint sends a keepalive after hearing from its
   peer (RFC 9000 section 10.1). So a peer that dies is dropped about 10 seconds after
   its last packet if it was talking a moment ago, and up to 15 seconds after a quiet
   spell. Do not tune this to make it exactly 10.
7. Relays default to the n0 public relays. A caller can override that.

## 5. Library API

Package `irohacp`, in `irohacp.go`. The package comment says, in a few lines, what
the package does. It exports exactly what this block declares, and nothing else.
The names and signatures are fixed, and the tests compare the package with the
block:

```go
const ALPN = "acp/1"

func Bind(ctx context.Context, opts ...iroh.Option) (*iroh.Endpoint, error)
func LoadKey(path string) (key.SecretKey, error)
func Dial(ctx context.Context, ep *iroh.Endpoint, ticket string) (net.Conn, error)
func AllowIDs(ids ...key.EndpointID) func(key.EndpointID) bool
func Serve(ctx context.Context, ep *iroh.Endpoint, allow func(key.EndpointID) bool, handle func(net.Conn)) error
func ServeAgent(ctx context.Context, ep *iroh.Endpoint, allow func(key.EndpointID) bool, newAgent func(*acp1.AgentSideConnection) acp1.Agent) error
func ConnectAgent(ctx context.Context, ep *iroh.Endpoint, ticket string, newClient func(*acp1.ClientSideConnection) acp1.Client) (*acp1.RemoteAgent, error)
```

Import paths: `acp "github.com/ironpark/acp-go"`, `acp1 "github.com/ironpark/acp-go/acp1"`,
and `github.com/tmc/go-iroh/{endpointticket,iroh,key,relay}`.

### Bind

Binds an iroh endpoint for ACP. It applies these options first:

- accept ALPN `acp/1`;
- relay mode `relay.ModeDefault()`;
- QUIC transport config with `MaxIdleTimeout` of 10 seconds.

Then it applies the caller's `opts`, so the caller's options win. The exception is
`iroh.WithALPNs`, which adds to `acp/1` rather than replacing it.

### LoadKey

Reads the key file at `path` and returns the secret key.

- If the file does not exist, generate 32 random bytes, create the parent directory
  with mode `0700` (`MkdirAll`), write the file with mode `0600`, and return the key
  made from those bytes.
- If the file exists, use its bytes as they are. Two calls on the same path return
  keys with the same public key. A file that is not 32 bytes makes
  `key.SecretKeyFromSlice` fail, and `LoadKey` returns that error, wrapped or not.
- Any other error is returned. The function never prints.

### Dial

Decodes `ticket` with `endpointticket.Decode`, then dials it with ALPN `acp/1`.
Returns the stream as a `net.Conn`. The returned value also has `CloseWrite() error`,
which closes only the send side. A malformed ticket returns the decoder's error.

### AllowIDs

Returns a function that reports whether an ID is one of `ids`. With no IDs, it
accepts nobody.

### Serve

Runs until `ctx` is done.

- `Serve` takes over `ep`'s accept loop through go-iroh's router. If the router cannot
  be set up, for example because something else already accepts on `ep`, `Serve`
  returns that error at once.
- For each bidirectional stream accepted on ALPN `acp/1` from an allowed peer,
  start a goroutine that calls `handle` with the stream, and close the stream when
  `handle` returns.
- For a peer that `allow` rejects: close its connection with code `1` and reason
  `not allowed`, and make the handler return an error whose text is
  `rejected <endpoint id>`. go-iroh's router logs that error at WARN on the
  process's default logger. That log line is how a rejected ID reaches stderr.
- When `ctx` is done, stop accepting, close every connection `Serve` accepted, which
  ends their streams, and return. The reference returns `net.ErrClosed`. A caller
  MUST NOT depend on the value, so `nil` is also fine.
- The reference does not return when `ep` is closed while `ctx` is still live. A
  caller stops it by cancelling `ctx`. A rebuild may also return when `ep` closes.
- `Serve` MUST NOT shut down `ep`. The caller made the endpoint and closes it.
- Rejection reaches a caller in two different ways. On a raw `Dial` stream, the next
  read fails with `Application error 0x1 (remote): not allowed`. Through
  `ConnectAgent`, acp-go turns the failure into `context canceled` on the first
  request, and the reason is lost. Both are as in the reference and neither is a bug
  to fix. `acp-client` uses `Dial`, so it shows the reason.

### ServeAgent

`Serve` where each stream gets a new acp-go agent. It builds the agent with
`newAgent`, over an acp-go stdio transport whose reader and writer are the stream,
and runs it until it ends or `ctx` is done. The error that ends one connection is
dropped. It only says why that one connection stopped.

### ConnectAgent

`Dial`, then an acp-go client connection over an acp-go stdio transport whose
reader and writer are the stream. It returns the `*acp1.RemoteAgent`.

## 6. `acp-server`

```
acp-server [-key file] -allow <client-id> [-allow <client-id> ...] <agent-command> [args...]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-key` | `<os.UserConfigDir()>/iroh-acp/server.key` | Key file; created if missing. Usage text: ``key `file`, created if missing``. |
| `-allow` | none, required | A client ID, as printed by `acp-client`. Repeatable. Usage text: ``client `id` to serve, as printed by acp-client (repeatable)``. |

Behavior, in order:

1. Parse flags with the standard `flag` package. An `-allow` value that is not a
   valid endpoint ID is a flag error, so the program exits with status 2 and the
   standard flag message.
2. If there is no agent command or no `-allow`, log
   `usage: acp-server -allow <client-id> [-allow ...] <agent-command> [args...]`
   with `log.Fatal` (stderr, exit status 1).
3. Load the key with `LoadKey`. Bind with `Bind` and `iroh.WithSecretKey`. Any error
   is logged with `log.Fatal`.
4. Wait for the endpoint to be online, for at most 10 seconds. Ignore a timeout: with
   no home relay, the ticket has direct addresses only.
5. Print the ticket and a newline on stdout. It is the only thing the server ever
   writes to stdout.
6. Serve with `AllowIDs` over all `-allow` IDs. For each stream, run the agent
   command as a child process with the stream as both its stdin and stdout, and the
   server's own stderr as its stderr. Set `WaitDelay` to 1 second so a stuck copy
   cannot block the exit. When the child ends, log `agent exited: <error or nil>`.
   The command name is `flag.Arg(0)`, the arguments are the rest, and both are run
   as given, with no shell.
7. If serving fails, log it with `log.Fatal`.

All logging uses the standard `log` package, so lines have its default timestamp
prefix.

## 7. `acp-client`

```
acp-client [-key file]            # print this client's ID
acp-client [-key file] <ticket>   # bridge stdio to the agent at ticket
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-key` | `<os.UserConfigDir()>/iroh-acp/client.key` | Key file; created if missing. Usage text: ``key `file`, created if missing``. |

Behavior:

1. Load the key with `LoadKey`. Any error is logged with `log.Fatal`.
2. With no argument, print the client's endpoint ID (64 hex characters) and a
   newline on stdout, then exit with status 0. It does not bind an endpoint.
3. Otherwise bind with `Bind` and `iroh.WithSecretKey`, then `Dial` the ticket in
   `flag.Arg(0)`. Arguments after the ticket are ignored. Any error is logged with
   `log.Fatal`.
4. Copy stdin to the stream. When stdin reaches EOF, call `CloseWrite` on the stream.
5. Copy the stream to stdout until the stream ends.
6. Shut the endpoint down, then exit with status 0 if the copy ended without error.
   If it ended with an error, log it with `log.Fatal`.

When the server rejects the client, the error text contains `not allowed`. It reaches
stderr through `log.Fatal` and the status is 1. Whether the error comes out of `Dial`
or out of the copy in step 5 depends on timing, and both paths must end this way.

## 8. Lifecycle

| Event | What must happen |
| --- | --- |
| The editor closes the agent's stdin | `acp-client` half-closes the stream. The remote agent sees EOF and exits. The server closes the stream. `acp-client` sees EOF and exits with status 0. Well inside acp-go's exit grace period. |
| The editor kills `acp-client` with SIGKILL | Nothing is sent. The server drops the connection 10 to 15 seconds later (section 4, item 6). The remote agent's stdin closes and it exits. The tests allow up to 15 seconds. |
| A peer that is not allowed connects | It gets no agent, its `acp-client` exits non-zero with `not allowed` on stderr, the server logs the `rejected <id>` line, and the server keeps serving. |
| Two allowed clients connect at once | Each gets its own agent process. |
| The server restarts with the same key file | It has the same endpoint ID. A client that dials the ticket from before the restart connects. |
| A quiet session | It stays up. Keepalives run every 5 seconds. |

## 9. Observable strings

The tests check these. Reproduce them exactly.

| Where | Text |
| --- | --- |
| ALPN | `acp/1` |
| Close reason for a rejected peer | `not allowed`, with code `1` |
| Server log for a rejected peer | contains `rejected <endpoint id>` |
| Server log when an agent ends | contains `agent exited` |
| Server usage error | the message contains `usage: acp-server`, after the default `log` timestamp (section 6) |
| Ticket on stdout | one line, matches `^endpoint[a-z2-7]+$` |
| Client ID on stdout | one line, matches `^[0-9a-f]{64}$` |

## 10. State and files

The only files the program writes are the two key files. `LoadKey` is the only code
that writes them. Key files are credentials: mode `0600`, and the containing
directory `0700` when `LoadKey` creates it. Tests pass `-key`, so they never touch
the user's real key files.

## 11. What the tests leave out

The held-out tests cover most of the sections above, including the size goal in
section 2. These are the parts they leave out, so a rebuild has to get them right
from this file.

- The exit status of the usage error. The tests check only that it is not zero.
- What `Serve` does with open connections when `ctx` is done.
- `Serve` handles more than one stream per connection. The tests use one.

## 12. Known limits

Keep these. They are documented in the README and are not bugs to fix in a rebuild.

- A killed client leaves its agent running for 10 to 15 seconds.
- Peers that cannot connect directly use n0's public relays by default. The
  library lets a caller pass its own relay options to `Bind`.
- An allowed client can do anything the agent can do on the server's machine.
- The allowlist is a static list of IDs. There is no config file, no revocation
  short of restarting the server, and no per-client policy. `Serve` takes any
  `allow` function, so a caller can supply one.
- The server has no graceful shutdown. It runs until it is killed.

## 13. Non-goals

Do not add: extra flags, config files, logging options, metrics, TLS or auth beyond
the endpoint ID check, a wire protocol on top of ACP, or a second transport.
