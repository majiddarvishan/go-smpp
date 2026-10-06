# Test coverage floor

Statement coverage is measured on every run of `scripts/coverage.sh` and compared with the minimums in
`.coverage-floor`. The point is not a high number; it is that coverage **cannot silently regress**: a
package falling below its floor fails the check, and lowering a floor is a visible, reviewable edit to a
file in version control rather than something that happens when tests are deleted.

## Running it

```sh
scripts/coverage.sh            # check; exit 1 on any failure
scripts/coverage.sh --suggest  # print measured values and suggested floors; changes nothing
GOFLAGS=-race scripts/coverage.sh   # same check under the race detector (needs cgo)
```

The script runs the whole test suite first (a failing test fails the check, because coverage from a
broken suite means nothing), then reports each package's coverage from its **own** tests and the module
total, which also credits cross-package coverage (a `session` test exercising `codec` counts for `codec`).

## Current floors

| Package | Floor | Measured (2026-10-05) |
| --- | ---: | ---: |
| `client` | 59 | 61.1 |
| `codec` | 62 | 64.8 |
| `encoding` | 72 | 74.5 |
| `message` | 73 | 75.8 |
| `protocol` | 54 | 56.8 |
| `server` | 73 | 75.7 |
| `session` | 80 | 82.3 |
| `transport` | 78 | 80.4 |
| module total | 71 | 73.3 |

Measured with a Go 1.22 toolchain on linux/amd64. Each floor is set 2 points below the measurement.
Run-to-run noise was under 0.5 points over five runs (the only moving package was `session`, 82.3 to 82.8,
from timing-dependent branches), and the same figures held under `-race -covermode=atomic`.

## Rules

- **Floors only go down on purpose.** If a change legitimately lowers coverage, edit `.coverage-floor` in the
  same commit and say why in the message. A reviewer will see the diff.
- **Raise floors when coverage rises.** The script prints a note when a package is 8 or more points above
  its floor. Use `--suggest` for the numbers.
- **New packages need a decision.** A package with statements and no floor fails the check. Give it a floor,
  or add an `exclude` line with the reason.
- **A floor for a package that no longer exists fails too**, so renaming or deleting a package cannot
  quietly shed its floor.
- **Excluded by design:** the root package (it only carries architecture, API and posture tests),
  `cmd/smpp-sim`, `examples/*` and `internal/perflab`. See the `exclude` lines for the reasons.

## What a floor does and does not tell you

Statement coverage says a line ran, not that anything checked its result. These floors are a guard against
regression, not evidence of correctness; the mutation checks recorded in `.claude/TASKS.md` are what tested
whether the tests can fail. A low floor on `protocol` (54) mostly reflects large generated-style tables of
encoders and decoders that are exercised by round-trip and fuzz tests rather than line by line.

## Running it in CI

Not wired in yet: CI changes are held for the final stage of the project. When that happens, the step is one
line, placed after "Unit tests" in `.github/workflows/ci.yml`:

```yaml
      - name: Coverage floor
        run: scripts/coverage.sh
```

Until then, run it locally before merging.
