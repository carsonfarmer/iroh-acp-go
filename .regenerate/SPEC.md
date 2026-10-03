# iroh-acp specification

This file states what iroh-acp must do. Two implementations follow it, iroh-acp-go in
Go and iroh-acp-rs in Rust, and both repositories hold this same file, byte for byte.
Most of it applies to both. A part marked Go or Rust applies to that implementation
only. MUST, SHOULD and MAY are used as in RFC 2119.

The file was first reconstructed from the Go code at commit 0fbc125, its README and
its tests. The Rust port was built from it, and the file was then reconciled with
both, so it describes the two reference implementations as they are.

A blind rebuild (see `PROMPT.md`) gets this file and nothing from either
implementation's source. If a rebuild fails the tests because this file was silent or
vague, fix the file in both repositories and run the rebuild again. A change here
changes both implementations, so run both spec suites before you commit it.

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

The deliverable is a library and two binaries, `acp-server` and `acp-client`, in Go or
in Rust. The two implementations speak the same protocol and read the same key files,
so a client from one works with a server from the other.

## 2. Inputs that are fixed

### Go

| Item | Value |
| --- | --- |
| Module path | `github.com/carsonfarmer/iroh-acp-go` |
| Go version and dependencies | As pinned in `go.mod` and `go.sum`. Do not change the `go` line or the direct requirements. `GOTOOLCHAIN=auto` fetches the toolchain. |
| iroh | `github.com/tmc/go-iroh` |
| ACP | `github.com/ironpark/acp-go` |
| Layout | `irohacp.go` (package `irohacp`), `cmd/acp-server/main.go`, `cmd/acp-client/main.go` |
| Formatting and lint | `gofmt`, and golangci-lint with the `.golangci.yml` you were given |
| License | MIT (unchanged) |

### Rust

| Item | Value |
| --- | --- |
| Crate | `iroh-acp`, so the library is `iroh_acp` |
| Rust version and dependencies | As pinned in `Cargo.toml` and `Cargo.lock`. Do not change the `[package]`, `[dependencies]` or `[lints]` tables. |
| iroh | `iroh`, and `iroh-tickets` for tickets |
| ACP | `agent-client-protocol` |
| Glue | `tokio`, and `tokio-util` for its `compat` and `task` modules |
| Layout | `src/lib.rs`, `src/bin/acp-server.rs`, `src/bin/acp-client.rs`. Your own tests go in `tests/`. `examples/` MAY hold an echo agent for them. |
| Formatting and lint | `rustfmt` with the `rustfmt.toml` you were given, and clippy with the `[lints]` table |
| License | MIT (unchanged) |

### Both

The goal is the fewest custom lines. The library stays under 100 lines of code and
each binary under 50. A line of code is a line that is neither blank nor a comment
that starts with `//`. Every file of the library or a binary counts, so moving code to
another file does not help. The Go count leaves out `_test.go` files. The Rust count
for the library takes every `.rs` file in `src/` outside `src/bin/`, unit tests
included, so Rust tests belong in `tests/`.

Everything else comes from those dependencies and the standard library. An
implementation MUST NOT copy code out of a dependency's source into the repository. It
MUST NOT add other dependencies, and in Rust that includes dev-dependencies.

Read the dependencies' source before using them. Their exported names are the truth,
and this file does not restate them.

## 3. Definitions

- **Endpoint ID.** The endpoint's Ed25519 public key, written as 64 lowercase
  hexadecimal characters. In Go it is a `key.EndpointID`, and `key.ParseEndpointID`
  reads that form. In Rust it is an `iroh::EndpointId`, whose `Display` writes that
  form and whose `FromStr` reads it.
- **Ticket.** One line of lowercase text that starts with `endpoint`. It carries the
  server's ID, its home relay and its current direct addresses. It grants nothing by
  itself. Go makes it with `endpointticket.Encode(ep.Addr())`, and Rust with
  `EndpointTicket::new(ep.addr()).to_string()`. Each implementation reads the other's
  tickets.
- **Client, server, agent.** The client is the ACP client process on the user's
  side (an editor). `acp-client` stands in for the agent on that side. `acp-server`
  runs the real agent as a child process on the other side.
- **Key file.** A file that holds exactly the 32 raw bytes of an Ed25519 secret key
  seed. No header, no encoding. A key file written by one implementation works in the
  other.
- **Config dir.** The directory Go's `os.UserConfigDir` returns. On macOS it is
  `$HOME/Library/Application Support`. Elsewhere it is `$XDG_CONFIG_HOME` if that is
  set and not empty, or else `$HOME/.config`.

## 4. Wire behavior

1. The ALPN is `acp/1`. It is the exported constant `ALPN`.
2. The dialer opens one bidirectional stream. The bytes on that stream are ACP's
   newline-delimited JSON-RPC in both directions. Neither side adds, removes,
   buffers to a boundary, or rewrites bytes.
3. The dialer closing its send side means "stdin EOF" to the agent. Go does this with
   `CloseWrite` and Rust with `SendStream::finish`. The dialer MUST NOT stop reading.
4. A server accepts a connection only from a peer whose endpoint ID its allow
   function accepts. The ID is authenticated by the QUIC TLS handshake. For any other
   peer, the server closes the connection with application error code `1` and the
   reason `not allowed`. It does not start an agent, even for a stream that arrived
   before the allow function returned.
5. A rejected peer MUST NOT stop the server from accepting other peers.
6. Connections use a maximum idle timeout of 10 seconds. iroh's default is 30. Both
   go-iroh and iroh send keepalives every 5 seconds, so a quiet, live connection stays
   open. QUIC restarts the idle timer when an endpoint sends a keepalive after hearing
   from its peer (RFC 9000 section 10.1). So a peer that dies is dropped about 10
   seconds after its last packet if it was talking a moment ago, and up to 15 seconds
   after a quiet spell. Do not tune this to make it exactly 10.
7. Relays default to the n0 public relays. A caller can override that.

## 5. Library API

Each implementation exports exactly what its block below declares, and nothing else.
The names and signatures are fixed, and the spec suite compares the code with the
block. The two APIs do the same jobs, described after the blocks, in the shapes their
dependencies suggest.

### Go declarations

Package `irohacp`, in `irohacp.go`. The package comment says, in a few lines, what the
package does.

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

### Rust declarations

The crate root, `src/lib.rs`, starts with a crate comment that says in a few lines
what the library does.

```rust
pub const ALPN: &[u8] = b"acp/1";
pub type Error = Box<dyn std::error::Error + Send + Sync>;
pub fn builder() -> Builder;
pub fn key_path(name: &str) -> PathBuf;
pub fn load_key(path: impl AsRef<Path>) -> io::Result<SecretKey>;
pub async fn dial(ep: &Endpoint, ticket: &str) -> Result<(SendStream, RecvStream), Error>;
pub fn allow_ids(ids: Vec<EndpointId>) -> impl Fn(EndpointId) -> bool + Clone + Send + 'static;
pub async fn serve<A, H, F>(ep: &Endpoint, allow: A, handle: H)
where
    A: Fn(EndpointId) -> bool + Clone + Send + 'static,
    H: Fn(SendStream, RecvStream) -> F + Clone + Send + 'static,
    F: Future<Output: Send> + Send + 'static;
pub async fn serve_agent<A: ConnectTo<Client>>(
    ep: &Endpoint,
    allow: impl Fn(EndpointId) -> bool + Clone + Send + 'static,
    new_agent: impl Fn() -> A + Clone + Send + 'static,
);
pub async fn connect_agent(ep: &Endpoint, ticket: &str) -> Result<impl ConnectTo<Client>, Error>;
```

The names come from these paths: `Builder`, `SendStream` and `RecvStream` from
`iroh::endpoint`; `Endpoint`, `EndpointId` and `SecretKey` from `iroh`; `Client`
and `ConnectTo` from `agent_client_protocol`; `PathBuf`, `Path` and `io` from `std`.

The tests take each declaration in `src/lib.rs` that starts with `pub` at the start of
a line, up to the `{` or `;` that ends its signature. Then they drop path prefixes
such as `std::io::`, parameter names, trailing commas and any whitespace that does not
sit between two words. So the names, the types, and the generic parameters and their
bounds MUST be written as they are here, in the same order and in the same place
(inline or in a `where` clause).

### Endpoint setup

An endpoint for ACP uses the n0 relays and a maximum idle timeout of 10 seconds. The
caller supplies the secret key.

- Go: `Bind` applies these options first: accept ALPN `acp/1`, relay mode
  `relay.ModeDefault()`, and a QUIC transport config with `MaxIdleTimeout` of 10
  seconds. Then it applies the caller's `opts`, so the caller's options win. The
  exception is `iroh.WithALPNs`, which adds to `acp/1` rather than replacing it.
- Rust: `builder` returns `Endpoint::builder(presets::N0)` with a transport config
  made by `QuicTransportConfig::builder()` with a maximum idle timeout of 10 seconds.
  Its other settings, such as the keepalive, stay at iroh's defaults. The caller adds
  its own options, such as the secret key, and binds it. The builder sets no ALPN.
  `serve` sets it, and `dial` passes it when it connects.

### Key files

`LoadKey` in Go and `load_key` in Rust read the key file at a path and return the
secret key.

- If the file does not exist, generate a new key, create the missing parent
  directories with mode `0700`, write the 32 bytes to the file with mode `0600`, and
  return the key.
- If the file exists, use its bytes as they are. Two calls on the same path return
  keys with the same public key. A file that is not 32 bytes is an error.
- Any other error is returned. The function never prints.

In Go, generate 32 random bytes and make the key with `key.SecretKeyFromSlice`, and
create the directories with `os.MkdirAll`. A file that is not 32 bytes makes
`key.SecretKeyFromSlice` fail, and `LoadKey` returns that error, wrapped or not.

In Rust, take the bytes from `SecretKey::generate`, and create the file so that it
fails if the file appeared meanwhile. Rust also exports `key_path(name)`, which
returns `<config dir>/iroh-acp/<name>` and reads `$HOME` with `std::env::home_dir`. Go
has no such function, and its binaries join `os.UserConfigDir()` with
`iroh-acp/<name>` themselves.

### Dial

Parse the ticket, connect to the endpoint in it with ALPN `acp/1`, open one
bidirectional stream and return it. A malformed ticket returns the parser's error.

- Go: `Dial` decodes the ticket with `endpointticket.Decode` and returns the stream
  as a `net.Conn`. The value also has `CloseWrite() error`, which closes only the
  send side.
- Rust: `dial` parses an `EndpointTicket` and returns the stream's two halves.

### Allow list

`AllowIDs` in Go and `allow_ids` in Rust return a function that reports whether an ID
is one of the given IDs. With no IDs, it accepts nobody.

### Serve

`Serve` in Go and `serve` in Rust accept ACP connections on an endpoint and hand each
stream to a handler.

- For a peer that `allow` accepts, accept bidirectional streams until the connection
  ends, and run the handler on each in a goroutine or task of its own.
- For a peer that `allow` rejects, close its connection with code `1` and reason
  `not allowed`, and write a log line on stderr that contains
  `rejected <endpoint id>`. Run the handler on none of its streams, not even one that
  arrived before `allow` returned. That log line is the library's only output.
- A rejected peer does not stop the server.
- It MUST NOT close the endpoint. The caller made the endpoint and closes it.

Rejection reaches a caller in two different ways. On a raw dialed stream, the next
read fails, and the error carries `not allowed`. Through `ConnectAgent` or
`connect_agent`, the first request fails, and the reason may be lost. Both are as in
the references and neither is a bug to fix. The client binaries dial raw streams, so
they can show the reason.

Go:

- `Serve` takes over `ep`'s accept loop through go-iroh's router and runs until `ctx`
  is done. If the router cannot be set up, for example because something else already
  accepts on `ep`, `Serve` returns that error at once.
- The handler gets the stream as a `net.Conn`, and `Serve` closes the stream when the
  handler returns.
- For a rejected peer, the router's protocol handler returns an error whose text is
  `rejected <endpoint id>`. go-iroh's router logs that error at WARN on the process's
  default logger, and that is the log line.
- When `ctx` is done, stop accepting, close every connection `Serve` accepted, which
  ends their streams, and return. The reference returns `net.ErrClosed`. A caller
  MUST NOT depend on the value, so `nil` is also fine.
- The reference does not return when `ep` is closed while `ctx` is still live. A
  caller stops it by cancelling `ctx`. A rebuild may also return when `ep` closes.
- On a raw `Dial` stream, a rejected read fails with
  `Application error 0x1 (remote): not allowed`. Through `ConnectAgent`, acp-go turns
  the failure into `context canceled` on the first request.

Rust:

- `serve` sets `ep`'s ALPNs to exactly `acp/1` and runs until `ep.accept()` returns
  `None`, that is until `ep` closes. Dropping the future also stops it accepting.
- For each incoming connection it spawns a task. The task finishes the handshake and
  calls `allow` with the peer's ID. The handler gets each stream's two halves.
- For a rejected peer, the task writes `rejected <endpoint id>` and a newline to
  stderr after it closes the connection, and returns. noq, the QUIC library under
  iroh, still yields streams that arrived before the close, and running the handler
  on one would serve the rejected peer.
- On a raw `dial` stream, a rejected read fails with a `ReadError` whose `Display` is
  `connection lost`. Its error source is the `ConnectionError`, which shows
  `not allowed` and the code.

### ACP on top

`ServeAgent` and `serve_agent` are `Serve` where each stream gets a new ACP agent.
`ConnectAgent` and `connect_agent` dial a ticket and set up the client side of ACP on
the stream. Both run ACP's stdio framing over the stream, unchanged. The error that
ends one agent connection is dropped. It only says why that one connection stopped.

- Go: `ServeAgent` builds each agent with `newAgent`, over an acp-go stdio transport
  whose reader and writer are the stream, and runs it until it ends or `ctx` is done.
  `ConnectAgent` calls `Dial`, then makes an acp-go client connection over the same
  kind of transport, and returns the `*acp1.RemoteAgent`.
- Rust: `serve_agent` connects each agent from `new_agent` to an
  `agent_client_protocol::ByteStreams` transport whose writer is the send stream and
  whose reader is the receive stream, using `tokio_util::compat`, and runs it until it
  ends. `connect_agent` calls `dial` and returns the same kind of transport. The
  caller passes it to `Client.builder().connect_with`.

## 6. `acp-server`

```text
acp-server [-key file] -allow <client-id> [-allow <client-id> ...] <agent-command> [args...]
```

| Flag | Default | Meaning | Go usage text |
| --- | --- | --- | --- |
| `-key` | `<config dir>/iroh-acp/server.key` | Key file, created if missing. | ``key `file`, created if missing`` |
| `-allow` | none, required | A client ID, as printed by `acp-client`. Repeatable. | ``client `id` to serve, as printed by acp-client (repeatable)`` |

The Go usage text is the usage string the Go binary gives the `flag` package, so its
`-h` prints it. The Rust binaries have no text per flag.

Both binaries read flags in the syntax of Go's `flag` package. Flags come first. A
flag is `-name value` or `-name=value`, and `--name` is the same as `-name`. Flags end
at the first argument that does not start with `-` and is not a flag's value. That
argument and every one after it are the agent command and its arguments. What `-` or
`--` does on its own is not fixed.

Behavior, in order:

1. Parse the flags. `-h` or `-help` prints help on stderr and exits with status 0. An
   unknown flag, a flag with no value, or an `-allow` value that is not a valid
   endpoint ID prints an error on stderr and exits with status 2.
2. If there is no agent command or no `-allow`, write a line that contains
   `usage: acp-server` on stderr and exit with status 1.
3. Load the key (section 5, key files) and bind an endpoint for ACP with it. Any
   error goes to stderr, and the status is 1.
4. Wait for the endpoint to be online, for at most 10 seconds. Ignore a timeout. With
   no home relay, the ticket has direct addresses only.
5. Print the ticket and a newline on stdout. It is the only thing the server ever
   writes to stdout.
6. Serve with an allow list of all `-allow` IDs. For each stream, run the agent
   command as a child process. The first word is the program and the rest are its
   arguments, run as given, with no shell. The child reads the stream as its stdin
   and its stdout goes to the stream. Its stderr is the server's stderr. Once the
   child exits, give the copy of its stdout at most 1 more second, so a stuck copy
   cannot hold the stream open. Then end the stream, so the client sees EOF, and log
   `agent exited: ` followed by the exit status or the error.
7. The server runs until it is killed. If serving fails, the error goes to stderr and
   the status is 1.

Go:

- Flags use the standard `flag` package with the usage texts in the table. Its
  messages are the help and errors of step 1.
- Logging uses the standard `log` package, so lines have its default timestamp
  prefix. Step 2 is
  `log.Fatal("usage: acp-server -allow <client-id> [-allow ...] <agent-command> [args...]")`,
  and steps 3 and 7 use `log.Fatal` too.
- The child is an `exec.Cmd` for `flag.Arg(0)` and the rest of `flag.Args()`. Its
  `Stdin` and `Stdout` are the stream, its `Stderr` is `os.Stderr`, and its
  `WaitDelay` is 1 second. The log line is `log.Print("agent exited: ", cmd.Run())`.
- `cmd.Run()` also waits for a read of the stream that is in progress, and
  `WaitDelay` does not cut that read short. So when the child exits while the
  client's side of the stream is open, the end of step 6 waits until the client
  sends more data or closes its side.

Rust:

- The flags are parsed by hand, since no flag crate is allowed. `-h`, an unknown flag
  and a flag with no value print
  `usage: acp-server [-key file] -allow <client-id> [-allow ...] <agent-command> [args...]`
  and a newline. A bad `-allow` value prints the parse error instead. Step 2 prints the
  same usage line.
- Log lines are plain lines on stderr, with no prefix. Errors are printed with their
  `Display`, and the binary exits with `std::process::exit`.
- The child is a `tokio::process::Command` with piped stdin and stdout. The server
  copies the receive stream into its stdin and its stdout into the send stream, and
  the send stream finishes when that copy ends. The log line shows the `ExitStatus` or
  the spawn error.

## 7. `acp-client`

```text
acp-client [-key file]            # print this client's ID
acp-client [-key file] <ticket>   # bridge stdio to the agent at ticket
```

| Flag | Default | Meaning | Go usage text |
| --- | --- | --- | --- |
| `-key` | `<config dir>/iroh-acp/client.key` | Key file, created if missing. | ``key `file`, created if missing`` |

Flags are read as in section 6.

Behavior, in order:

1. Parse the flags. `-h` or `-help` prints help on stderr and exits with status 0. An
   unknown flag or a flag with no value prints an error on stderr and exits with
   status 2.
2. Load the key. Any error goes to stderr, and the status is 1.
3. With no argument, print the client's endpoint ID (64 hex characters) and a newline
   on stdout, then exit with status 0. It does not bind an endpoint.
4. Otherwise bind an endpoint for ACP with the key, then dial the ticket in the first
   argument. Arguments after the ticket are ignored. Any error goes to stderr, and the
   status is 1.
5. Copy stdin to the stream. When stdin reaches EOF, close the send side of the
   stream.
6. Copy the stream to stdout until the stream ends.
7. Close the endpoint, and wait at most 1 second for the server to answer the close.
   If the copy in step 6 ended without error, exit with status 0. If it failed, the
   error goes to stderr and the status is 1.

When the server rejects the client, the error on stderr contains `not allowed` and the
status is 1. Whether the error comes out of the dial or out of the copy in step 6
depends on timing, and both paths must end this way.

Go:

- Flags use the standard `flag` package with the usage text in the table.
- Each error is logged with `log.Fatal`. Step 5 calls `CloseWrite`, and step 7 calls
  `ep.Shutdown`, which does not wait for an answer.

Rust:

- `-h`, an unknown flag and a flag with no value print
  `usage: acp-client [-key file] [ticket]` and a newline.
- Step 5 calls `SendStream::finish`, and step 7 calls `Endpoint::close` under a
  1-second `timeout`. A go-iroh server does not answer a close, which QUIC allows, and
  without the timeout `close` waits about 9 seconds for it.
- When the copy fails, the client prints the error's source, or the error itself if
  it has no source. The `ReadError` itself only says `connection lost`.
- The client exits with `std::process::exit`, because a read of stdin may still be
  blocked, and tokio's runtime waits for it when `main` returns.

## 8. Lifecycle

| Event | What must happen |
| --- | --- |
| The editor closes the agent's stdin | `acp-client` closes its send side. The remote agent sees EOF and exits. The server ends the stream. `acp-client` sees EOF and exits with status 0 within 2.5 seconds, half of the 5 seconds acp-go gives an agent to exit before it kills it. |
| The editor kills `acp-client` with SIGKILL | Nothing is sent. The server drops the connection 10 to 15 seconds later (section 4, item 6). The remote agent's stdin closes and it exits. The tests allow up to 15 seconds. |
| A peer that is not allowed connects | It gets no agent, its `acp-client` exits with status 1 and `not allowed` on stderr, the server logs the `rejected <id>` line, and the server keeps serving. |
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
| Server with no agent command or no `-allow` | stderr contains `usage: acp-server`, and the status is 1 |
| Client rejected by the server | stderr contains `not allowed`, and the status is 1 |
| `-h` | the status is 0 |
| A flag error | the status is 2 |
| Ticket on stdout | one line, matches `^endpoint[a-z2-7]+$` |
| Client ID on stdout | one line, matches `^[0-9a-f]{64}$` |

A run that ends in a usage error, a flag error or `-h` writes nothing on stdout.

Log lines MAY carry a prefix, such as a timestamp. The wording of the help, of flag
errors, of the rest of the usage line and of the text after `agent exited: ` is not
fixed. The Go and Rust notes in sections 6 and 7 say what each reference prints, and
the Go usage texts in the flag tables are fixed for Go.

## 10. State and files

The only files the programs write are the two key files, and only the key loading
code in section 5 writes them. Key files are credentials. They have mode `0600`, and
the directories the key loading code creates have mode `0700`. A run that ends in a
usage error, a flag error or `-h` writes no key file.

Both implementations use the same default key paths, so a user's Go and Rust binaries
share keys. Tests pass `-key`, or point `$HOME` at a temporary directory, so they
never touch the user's real key files.

## 11. What the tests leave out

The held-out spec suites cover most of the sections above, including the size goal in
section 2. These are the parts they leave out, so a rebuild has to get them right
from this file.

- What happens to open connections when serving stops, when `ctx` is done in Go or
  the `serve` future is dropped in Rust.
- Serving more than one stream per connection. The tests use one.
- An agent that exits while the client's side of the stream is still open.
- The default key path when `$XDG_CONFIG_HOME` is set.

## 12. Known limits

Keep these. They are documented in the README and are not bugs to fix in a rebuild.

- A killed client leaves its agent running for 10 to 15 seconds.
- Peers that cannot connect directly use n0's public relays by default. A caller of
  the library can set its own relays, through `Bind`'s options in Go or on the
  builder in Rust.
- An allowed client can do anything the agent can do on the server's machine.
- The allowlist is a static list of IDs. There is no config file, no revocation
  short of restarting the server, and no per-client policy. `Serve` and `serve` take
  any `allow` function, so a caller can supply one.
- The server has no graceful shutdown. It runs until it is killed.
- Key file modes are Unix modes. The Rust implementation builds only on Unix.
- When an agent exits while the client's side of the stream is still open, the Go
  server ends the stream only once the client sends more data or closes its side.
  The Rust server ends it within a second.

## 13. Non-goals

Do not add extra flags, config files, logging options, metrics, TLS or auth beyond the
endpoint ID check, a wire protocol on top of ACP, a second transport, or a dependency
for flags, errors, logging or paths.
