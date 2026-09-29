# AGENTS.md

Instructions for coding agents working on iroh-acp-go. [README.md](README.md) covers
what the project does and how to use it.

## The project

A Go library and two binaries that carry the Agent Client Protocol over iroh, so an
editor on one machine can use an agent on another. Nearly all the work is done by
[go-iroh](https://github.com/tmc/go-iroh) and [acp-go](https://github.com/ironpark/acp-go).
This repository holds the glue.

| Where | What it is |
| --- | --- |
| `irohacp.go` | The library, package `irohacp`. |
| `cmd/acp-server/`, `cmd/acp-client/` | The two binaries. |
| `irohacp_test.go` | The tests, for the library and for the built binaries. |
| `.github/` | CI for tests, golangci-lint and govulncheck, and the Dependabot config. |

## Commands

```bash
go build ./...
go vet ./...
go test -short ./...                      # loopback only, no network
go test -race -shuffle=on -count=1 ./...  # the full suite, including the n0 relays
golangci-lint run                         # the config is in .golangci.yml
```

Run `gofmt`, the full suite and golangci-lint before you call a change done. Without
a local golangci-lint, run
`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version> run` with the
version from `.github/workflows/lint.yml`.

## Plan before you build

For a new feature or any change in behavior, use the
[grill-me](https://github.com/mattpocock/skills/tree/main/skills/productivity/grill-me)
skill to interview the user before you write code. grill-me is the command a person
types. It runs the
[grilling](https://github.com/mattpocock/skills/tree/main/skills/productivity/grilling)
skill, which an agent can start by itself. Both are in
[mattpocock/skills](https://github.com/mattpocock/skills), whose README shows how to
install them. Keep going until every open question has an answer from the user.

A change in behavior updates the code, its tests and `README.md` together.

## Writing Go

Use the Go skills from
[samber/cc-skills-golang](https://github.com/samber/cc-skills-golang). Install all of
them, because they refer to each other. The ones this code needs most:

- [golang-code-style](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-code-style),
  [golang-naming](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-naming)
  and [golang-documentation](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-documentation)
- [golang-error-handling](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-error-handling),
  [golang-context](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-context)
  and [golang-concurrency](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-concurrency)
- [golang-testing](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-testing),
  [golang-safety](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-safety)
  and [golang-modernize](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-modernize)
- [golang-cli](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-cli)
  for the two binaries, and
  [golang-lint](https://github.com/samber/cc-skills-golang/tree/main/skills/golang-lint)
  for golangci-lint

Rules for this repository:

- **Stay inside the size budget.** The library stays under 100 lines of code and each
  binary under 50, counting lines that are neither blank nor comments in all of a
  package's files. CI fails when one goes over. When code grows, look in go-iroh or acp-go for something
  that already does the job.
- **Read the dependency source before you call it.**
  `go list -m -f '{{.Dir}}' github.com/tmc/go-iroh github.com/ironpark/acp-go` prints
  where it is. Do not guess an API from memory.
- **Treat the interface as a contract.** Other people's clients and servers depend
  on the exported signatures, the ALPN `acp/1`, the flags, stdout and the log lines.
  CI checks them, and they change only when the user asks.
- **Keep versions in one place.** Go, module and tool versions live in `go.mod`,
  `go.sum` and the workflow files. Do not repeat them in docs, comments or tests.
- **Ask before adding a direct dependency.**

## Tests

- `irohacp_test.go` uses only the exported API and the built binaries. A test of
  internals goes in a test file of package `irohacp`.
- Never loosen a test to make code pass. If a test is wrong, tell the user before you
  change it.
- Tests use `t.TempDir()` and pass `-key` to the binaries. They must never read or
  write the real key files in the user's config directory.
- The full suite dials through the n0 public relays, so it needs the network.
  `-short` skips those tests.

## Upstream bugs

Report and fix a bug in go-iroh or acp-go upstream. Carry a workaround here only
until the fix is released, and remove it in the commit that updates the dependency.

## Commits

- Keep commits small. The subject is a short imperative sentence, and the body says
  why.
- Never bypass git hooks with `--no-verify`.
- Key files are credentials. `*.key` is ignored, so never force one in or print its
  contents.
- Do not push, tag or open a pull request unless the user asks.

## Writing

Docs and comments use plain, direct prose: short sentences, no em dashes and no hype.
Do not name the maintainer in the docs.
