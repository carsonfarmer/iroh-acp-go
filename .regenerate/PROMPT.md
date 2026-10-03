# Regenerate iroh-acp-go

You are rebuilding a small Go project from its specification. You have not seen the
existing implementation and you must not look for it. Someone will check your result
against tests you cannot see.

## Your inputs

The workspace holds:

- `SPEC.md`. The behavior the result must have. It is your source of truth. It
  covers a Go and a Rust implementation. You are building the Go one, so the parts
  marked Go apply and the parts marked Rust do not.
- `DECISIONS.md`. Why the design is the way it is. Use it to choose between options
  when `SPEC.md` leaves room.
- `go.mod` and `go.sum`. They pin the toolchain and the dependencies. Do not change
  the two direct requirements or the `go` line.
- `LICENSE` and `.golangci.yml`. Keep them as they are.

## What to produce

1. `irohacp.go`, package `irohacp`, at the module root.
2. `cmd/acp-server/main.go` and `cmd/acp-client/main.go`.
3. `irohacp_test.go`, your own tests for what you built (see the tests section).

Nothing else. No extra packages, no README, no CI files.

## How to work

1. Read `SPEC.md` and `DECISIONS.md` in full before writing any code.
2. Read the source of the two dependencies in the Go module cache before you call
   them. Run `go mod download`, then
   `go list -m -f '{{.Dir}}' github.com/tmc/go-iroh github.com/ironpark/acp-go`
   prints where they are. Do not guess their APIs from memory. Their `examples/`
   directories show how to use them. Do not copy code from them into the project.
3. Write the smallest program that satisfies `SPEC.md`. The goal is the fewest custom
   lines. The library must stay under 100 non-comment, non-blank lines and each
   command under 50. The check counts every file in the package. Most of the work is
   already done by go-iroh and acp-go. If a function is getting long, look in the
   dependencies for something that does it.
4. Write idiomatic Go for the version in `go.mod`. Use `slices`, `t.Context()` and
   the other current standard library features where they make the code shorter.
   Comment exported names and the reason for anything surprising. Do not comment the
   obvious.
5. Run `gofmt`, `go vet ./...` and `go build ./...` as you go. Run
   `go test -race -shuffle=on ./...` before you finish. If `golangci-lint` is
   installed, run it with the `.golangci.yml` you were given.

## What you must not read

- Any other copy of this project, including one in a Go module cache, a parent
  directory, a sibling directory or a git remote. If you find one, close it and tell
  me in your report.
- The upstream `iroh-acp-go` repository.
- Any test file that is not one you wrote.

## Tests

Write your own tests from `SPEC.md`. Cover what a reader would want proved: a client
gets an agent through the library, a peer that is not allowed gets nothing and the
server keeps serving, two clients at once, key loading, and the built binaries behaving
the way sections 6 to 8 of the spec describe. Use the loopback address and disabled
relays for tests that need no network. Tests must never touch the real key files, so
pass `-key` to the binaries.

Your tests stay with your code if it is kept. They run with it, and a rebuild whose
own tests fail is not kept. A separate spec suite, which I am not showing you, also
decides whether it is kept:

- It is held out on purpose. If you had it, you could fit the code to the tests and
  stop reading the spec closely. The point of this exercise is to find out whether
  `SPEC.md` is enough to build the project. A rebuild that passes tests it was
  shown proves less.
- The suite compares your exported declarations with the Go block in section 5 of
  the spec, and your `-h` output with the Go usage texts in sections 6 and 7. It
  talks to your library through go-iroh itself, and runs your binaries by name with
  the `-key=` and `-allow=` flags. That is why the spec fixes those exactly.
- Your code and your tests are built against the `go.mod` you were given.
- If the suite fails, the spec gets fixed. The tests do not get loosened.

## Where the spec is silent

`SPEC.md` is not perfect. When it says nothing, or two sections disagree, choose the
smallest behavior that fits `DECISIONS.md` and the protocols involved, and write it
down. Do not stop to ask.

## When you finish

Reply with a short report and nothing else:

1. The files you wrote, with the non-comment, non-blank line count of each.
2. The output of `go vet ./...` and `go test -race -shuffle=on ./...`.
3. Every place `SPEC.md` was silent, vague or contradictory, and what you chose.
4. Anything you read outside the workspace and the two dependencies.
5. Anything in your code you are unsure about.

Do not commit and do not push.
