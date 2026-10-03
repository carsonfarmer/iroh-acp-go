# Provenance

This is the provenance record from Chad Fowler's
[Phoenix Primitives](https://aicoding.leaflet.pub/3mjfruwwuck2d): what produced the
reference implementation, and a ledger of every regeneration since.

Anything marked **unknown** was not written down when it happened, and nothing was
filled in from memory. A gap left visible tells the next reader what a rebuild cannot
reproduce.

## The reference implementation

The code was last changed at commit `0fbc125` (2026-09-29). The commits after it add
`.regenerate/` and leave the library and the commands as they were.

| Item | Value | Source |
| --- | --- | --- |
| Built | 2026-09-25 to 2026-09-29, first commit `d29e505` | `git log` |
| Model | Claude Opus 5.5, named in the trailer of all 8 commits | commit trailers |
| Harness | Claude Code, one long session | session log, local only |
| Design interview | The [grill-me](https://github.com/mattpocock/skills/tree/main/skills/productivity/grill-me) skill, run before the build | session log, local only. No question or answer was kept. |
| Go skills | [`samber/cc-skills-golang`](https://github.com/samber/cc-skills-golang) | session log, local only. Commit SHA at build time: **unknown**. |
| Go toolchain and dependencies | pinned in `go.mod` and `go.sum` | `go.mod`, `go.sum` |
| Brief | not published | |
| Lint | golangci-lint, config in `.golangci.yml` | `.github/workflows/lint.yml` |
| Vulnerability scan | govulncheck, weekly and on every push | `.github/workflows/security.yml` |
| Skill versions, temperature, other model settings | **unknown** | not recorded |

## Changes during the build

Requests made during the build session changed the design. They added the access
control hook (`Serve` takes an `allow` function), the persistent key files, the 10
second idle timeout and the CI setup. All of these landed before the first commit, so
the history does not show them one by one. The requests are in the session log only.

## Upstream problems found during the build

| Problem | Fix | Workaround removed |
| --- | --- | --- |
| acp-go issue 11 | acp-go pull request 12, merged | commit `40819dd` |
| go-iroh issue 25 | go-iroh v0.2.2 | commit `0fbc125` |

## Regeneration ledger

One row per regeneration attempt, including the ones that failed. A failed row is the
most useful kind, because it points at a gap in `SPEC.md`.

Fill in a row after each check of a rebuild. The first two runs used `verify.sh`, a
script that the spec suite in this directory has since replaced.

| Date | Mode | Model and harness | Inputs | Result | Spec gaps found | Spec changes |
| --- | --- | --- | --- | --- | --- | --- |
| 2026-09-29 | blind | `claude-sonnet-5-5` as a subagent in Claude Code, about 11 minutes | The draft `SPEC.md`, `DECISIONS.md` and `PROMPT.md` before any edit from this run, the `0fbc125` pins, an isolated module cache. No `reference-1` tag existed yet. | `verify.sh`: 34 passed, 0 failed, 2 skipped (golangci-lint and govulncheck were not installed). Size 95, 49 and 44 code lines. Two runs of the killed-client check took about 15 and about 10 seconds. | Usage line: section 9 said the text starts with `usage: acp-server` and section 6 put a timestamp first. `Serve`: return value on `ctx` done, and whether it shuts the endpoint down. `ConnectAgent` hides the reject reason and `Dial` shows it. `LoadKey` error for a bad key length. | Sections 4, 5 and 9 of `SPEC.md`. The next run used them. |
| 2026-09-29 | blind | `claude-sonnet-5-5` as a subagent in Claude Code, about 16 minutes | `SPEC.md`, `DECISIONS.md` and `PROMPT.md` with the changes from the run above, not yet committed. The `0fbc125` pins, an isolated module cache. No `reference-1` tag existed yet. The agent saw the name of the `ref` directory next to its workspace in a listing, and says it did not open it. | `verify.sh`: 30 passed, 0 failed, 2 skipped (golangci-lint and govulncheck were not installed). Since the run above, one `go.mod` comparison replaces five version checks. Size 93, 49 and 43 code lines. The killed-client check took about 15 seconds. The agent's own tests failed once in six full runs, when a dial through the relays timed out after about 11 seconds. | `Serve`: whether it returns when `ep` closes first, what happens to open connections when `ctx` is done, and the error when the router cannot start. Arguments after the `acp-client` ticket. The agent also misread `Bind`, taking `iroh.WithALPNs` to replace `acp/1` when it adds to it. | Sections 5 and 7 of `SPEC.md`. Not yet rerun. |
| 2026-10-02 | blind | `claude-sonnet-5-5` as a subagent in Claude Code, about 36 minutes. The harness also gave it this repository's `AGENTS.md` and recent commit subjects, and its shell started in this checkout. It reports reading nothing outside its workspace and its module cache, which also held the Go toolchain's source. | Reference, `SPEC.md`, `DECISIONS.md`, `PROMPT.md` and pins at `d9fae59`, an isolated module cache. The spec was the one shared with iroh-acp-rs. | Passed. Its own tests and the spec suite passed with `-race -shuffle=on`, and golangci-lint found 0 issues. Size 88, 49 and 43 code lines. Its own tests retry a dial that times out on the way through a distant relay, up to 4 times. | Step 6 of section 6 said the server ends the stream once the agent exits. The Go form that section requires does that only once the client writes or closes its side. | Sections 6, 11 and 12 of `SPEC.md`, D14 and the README caveats, in `e7aa7ae`. |

- **Mode** is `blind` or `guided`, as [`README.md`](README.md) defines them.
- **Inputs** names the commit exported as the reference, the commit of `SPEC.md`,
  `DECISIONS.md` and `PROMPT.md` that the run used, and the dependency pins.
- **Result** says whether the rebuild's own tests and the spec suite passed, names
  any that failed, and gives the line counts from `TestSize`.
