# Regenerating iroh-acp-go

iroh-acp-go is small enough to rebuild from a description. This directory holds that
description, a prompt for the agent that does the rebuilding, and a suite of tests
that checks any implementation against the description. The project outside this
directory, the code and its own tests, is one implementation. It is not the only
one, and a good rebuild may replace it.

The approach follows Chad Fowler's
[Regenerative Software](https://aicoding.leaflet.pub/3majnyfydzs2y) and
[The Phoenix Primitives](https://aicoding.leaflet.pub/3mjfruwwuck2d), and the
[regenerative software](https://aipatternbook.com/regenerative-software) entry in the
AI pattern book. None of them prescribes a layout, so the files and the two modes
below are this project's own.

## What is kept and what is replaceable

| Kept | Where | Role |
| --- | --- | --- |
| Specification | [`SPEC.md`](SPEC.md) | What the program must do. Exact API, flags, wire behavior and observable strings. |
| Decision log | [`DECISIONS.md`](DECISIONS.md) | Why the design is the way it is. It settles what the spec leaves open. |
| Prompt | [`PROMPT.md`](PROMPT.md) | The instructions to give a fresh agent. |
| Spec suite | [`acceptance_test.go`](acceptance_test.go), [`api_test.go`](api_test.go) | Checks an implementation against `SPEC.md`. It compares the exported declarations and the flags with the spec's text, talks to the code through go-iroh itself, drives the built binaries and counts lines. It uses nothing internal, so it can judge this code or a rebuild, and CI runs it on this code. |
| Pins | [`../go.mod`](../go.mod), [`../go.sum`](../go.sum) | Toolchain and dependency versions. |
| Provenance | [`PROVENANCE.md`](PROVENANCE.md) | What made the reference, and a ledger of every rebuild. |

`SPEC.md` and `DECISIONS.md` are the same files in iroh-acp-rs, a Rust implementation
of the same spec. A change to either goes into both repositories, and both spec
suites run on it before it is committed.

The replaceable part is the project itself: `irohacp.go` and the two `main.go` files,
178 lines of code in all, and `irohacp_test.go`, their tests.

## How fast each part changes

The pattern book asks for named pace layers: how often each layer may change, so that
a rebuild of the implementation does not drag the contract along.

| Layer | What is in it | How often it changes |
| --- | --- | --- |
| Contract | ALPN `acp/1`, byte-for-byte framing, close code 1 with reason `not allowed`, the flags, the exported signatures, and the spec suite that checks them | Rarely, and with a new spec version. A change breaks other people's clients and servers. |
| Design | `DECISIONS.md`, the size budget, the security model | When someone reopens a decision, and the entry changes with it. |
| Pins | `go.mod`: the Go version and both dependencies | When upstream releases. Dependabot proposes it. |
| Implementation | `irohacp.go`, the two `main.go` files and `irohacp_test.go` | Whenever a rebuild passes its own tests and the spec suite. |

## Two ways to regenerate

**Blind.** The agent gets `SPEC.md`, `DECISIONS.md`, `PROMPT.md`, `go.mod`, `go.sum`,
`LICENSE` and `.golangci.yml`. It does not get the existing code, its tests or the
spec suite. It writes its own tests. Then you run its tests and the spec suite on
what it built. Use this to find out whether the spec is enough. A failure is a spec
gap, and the fix goes into `SPEC.md`.

**Guided.** The agent gets the whole repository, both sets of tests included, and is
asked to rewrite the code and its tests. Use this for routine work, such as adopting
a new release of a dependency. The tests do the checking.

## Run it

You need Go (with `GOTOOLCHAIN=auto`, the go command fetches the version `go.mod`
asks for) and an agent that can edit files and run commands. From the repository
root:

1. Export the reference, here `HEAD`, make a workspace with the blind inputs, and
   fill a module cache that holds only the two dependencies. A copy of this module
   may sit in your usual cache, and a blind agent must not find it.

   ```bash
   ref=$(mktemp -d) ws=$(mktemp -d) cache=$(mktemp -d)
   git archive HEAD | tar -x -C "$ref"
   (cd "$ref" && cp go.mod go.sum LICENSE .golangci.yml .regenerate/SPEC.md \
     .regenerate/DECISIONS.md .regenerate/PROMPT.md "$ws/")
   (cd "$ws" && GOMODCACHE="$cache" GOFLAGS=-modcacherw go mod download)
   ```

   For a guided run, copy all of `$ref` into `$ws` instead.
2. Start a fresh agent session in `$ws` with `GOMODCACHE="$cache"` set. Give it the
   text of `PROMPT.md`, and tell it to work only inside `$ws`.
3. When it reports, check the result, whatever the agent says about it. Put its code
   and its tests in place of the reference's, then run both its tests and the spec
   suite:

   ```bash
   rm -r "$ref"/*.go "$ref/cmd"
   (cd "$ws" && cp -R *.go cmd "$ref/")
   (cd "$ref" && go vet ./... ./.regenerate &&
     go test -race -shuffle=on -count=1 ./... ./.regenerate)
   ```

   The build uses the reference's `go.mod`, so a rebuild that needs other pins or
   another package does not build. `go test -run TestSize -v ./.regenerate` prints
   the line counts for the ledger. CI also runs golangci-lint and govulncheck, so run
   them too if you have them.
4. Add a row to the ledger in [`PROVENANCE.md`](PROVENANCE.md). Record failures too.

## Rules for a fair run

- **Do not loosen the spec suite or fix the candidate by hand to make a rebuild pass.**
  Change the spec, then run the rebuild again.
- **Keep the agent away from the existing implementation.** A blind run only means
  something if the agent never saw the code. That includes the Go module cache, where
  a copy of this module may sit. Use an isolated `GOMODCACHE`, as above. An agent with
  a shell can still read outside its workspace, so for a run that must hold up, use a
  container with no other copy of the module in it.
- **Record what the run used.** Model, harness, prompt and spec versions go in the
  ledger. Without them, nobody can tell one run from another.

## What a passing run shows

A rebuild that passes the spec suite has the same API, the same flags, the same
behavior on the wire and the same visible strings as the reference, as far as the
suite reaches. `SPEC.md` lists, in section 11, what it does not reach. The badge in the README
points here so a reader can check the last ledger row and decide how much that run
proves.

## Working on the suite

The go command leaves dot directories out of `./...`, so name `./.regenerate` in
every command, as CI does. `go mod tidy` ignores it too, so the suite may import only
modules the project itself needs.
