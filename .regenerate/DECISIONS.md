# Design decisions

A rebuild reads this file for the choices `SPEC.md` leaves open. Each entry gives the
decision and the reason for it. Like `SPEC.md`, this file is the same in iroh-acp-go
and iroh-acp-rs.

- **Recorded** means the reason is written down in a README, a code comment or a
  commit message.
- **Inferred** means the reason was worked out from the code afterwards. Check these
  before you rely on them.

## Goal

D1. **Build the smallest thing that does the job.** The goal was as few custom lines
as possible, with other libraries doing the rest. The README states the result as a
budget: the library under 100 lines and each binary under 50. Recorded (README
intro).

D2. **Lean on an iroh library and an ACP library.** Identity, encryption, hole
punching, relay fallback and the ACP message types all come from them: go-iroh and
acp-go in Go, iroh and agent-client-protocol in Rust. Each project holds the glue.
Recorded (README intro).

## Wire and framing

D3. **Send ACP's bytes unchanged.** Each ACP connection is one bidirectional QUIC
stream carrying newline-delimited JSON-RPC as ACP does on stdio. So any stdio agent
and any ACP client work at either end without changes. Recorded (README, "How it
works"; package comment).

D4. **Stdin EOF half-closes the stream.** The agent sees EOF when the editor closes
its stdin, as it would locally. Recorded (README, "Lifecycle").

## Access control

D5. **Check the peer's ID before starting anything.** The QUIC handshake proves the
client holds the private key for its ID, so an allowlist of IDs is enough. There are
no passwords or tokens. Recorded (README, "Security model").

D6. **The allowlist is a function.** `AllowIDs` and `allow_ids` are the usual one, and
any other policy is one function away. Recorded (README, "Library").

D7. **Tell a rejected client why, and log its ID.** The client sees why it was turned
away and the operator sees who knocked. Inferred from the README's mention of both.

D8. **A rejected peer gets no agent and does not stop the server.** A stranger must
not be able to take the server down. This covers a stream the stranger sent before
`allow` returned. go-iroh never yields one from a closed connection, but noq, the QUIC
library under Rust's iroh, does, and an early Rust port served it. Recorded (commits
f9ec635 and 0fbc125).

## Keys and tickets

D9. **Keys live in files, created on first use.** IDs and tickets then survive
restarts. Recorded (`LoadKey` comment; README, "Identity").

D10. **Key files default to the user's config directory.** Tests and scripts pass
`-key`, so they never touch the real files. Recorded (README, "Commands" and
"Development").

D11. **`acp-client` with no ticket prints its ID.** One binary does both jobs, so
there is no third command. Inferred from the code.

## Lifecycle

D12. **The idle timeout is 10 seconds, down from iroh's 30.** Zed stops agents with
SIGKILL, so `acp-client` cannot say goodbye, and the remote agent should not outlive
it by long. Keepalives keep a quiet, live session up. Recorded (`Bind` and `builder`
comments; README, "Lifecycle").

D13. **One agent process per connection.** When the connection ends, so does the
agent. Recorded (README, "Lifecycle").

D14. **Bound the wait for the agent's I/O.** The stream is the process's stdin and
stdout, so a copy can outlive the process and hold up its exit. Go's `os/exec` has
`WaitDelay` for this, and the Rust server gives the copy the same second. Inferred
from the `os/exec` documentation for `WaitDelay`. `WaitDelay` does not bound a read
of the stream as the process's `Stdin`, so the Go server has a limit that the Rust
server does not (SPEC section 12). A blind rebuild found it, and the `os/exec`
documentation for `Stdin` confirms it.

D15. **Do not wait long for a home relay.** The server prints its ticket anyway, and
that ticket carries direct addresses only. Recorded (code comment in `acp-server`).

D16. **Stdout carries only the ticket. Everything else goes to stderr.** Inferred:
it keeps stdout easy to capture in a script.

## Connectivity

D17. **Default to the n0 public relays.** Callers can override them, through `Bind`'s
options in Go or the builder in Rust, and the README says to use your own relays in
production. Recorded (README, "Caveats").

## Tests and tooling

D18. **Each project's tests live with its code, and a spec suite in `.regenerate/`
checks any implementation against `SPEC.md`.** The suite drives only the exported
API, the built binaries and the iroh library itself. It compares the exported
declarations, and in Go the flags, with the text of the spec, and it checks the size
budget. CI runs it on the current code, so the code and the spec cannot drift apart
unnoticed. A rebuild replaces the code and its tests together. Recorded (README,
"Development" and "Regenerating this project").

D19. **CI runs the tests, a linter and a vulnerability scan, and Dependabot keeps
dependencies and actions current.** Go uses golangci-lint and govulncheck, and Rust
uses clippy and cargo-audit. Versions live in `go.mod`, `Cargo.toml`, the lock files
and the workflow files, nowhere else. Recorded (`.github/`, `.golangci.yml`, the
`[lints]` table).

D20. **Fix upstream bugs upstream.** The Go build found acp-go issue 11 and go-iroh
issue 25. Each was fixed upstream, and the Go repository carried a workaround only
until the fix landed. Recorded (commit messages).

## Two implementations

D21. **Two implementations share one spec.** The spec is the part that lasts, and the
code is one way to meet it. Building the same thing in a second language tests
whether the spec says enough without the first one's code. So both repositories hold
the same `SPEC.md` and this file, and a change to either goes into both. Recorded
(the shared `SPEC.md`).

D22. **The Go CLI is the contract, and the Rust binaries match it.** The Go binaries
were published first, and other people's scripts depend on their flags and exit
statuses. Rust's standard library has no flag parser and the spec allows no flag
crate, so the Rust binaries parse Go's flag syntax by hand. Help text, error wording
and log prefixes stay free, since matching Go's exactly would cost more lines than
the budget has. Recorded (the shared `SPEC.md`, sections 6 and 9).

D23. **Mixed pairs are checked by hand, not in CI.** A Go client must work with a Rust
server, and the other way round. Each repository's CI tests only its own code,
because a test that builds both would tie the two together. So the mixed pairs are
run by hand when the spec changes, and each failure becomes a line in the spec. The
first run found the Rust client waiting 9 seconds for a go-iroh server to answer its
close. Recorded (the shared `SPEC.md`, section 7).
