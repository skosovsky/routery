# Repository verification

Make invokes standard Go commands and golangci-lint directly. Modules are discovered
from go.mod files, excluding hidden directories and vendor. All commands use
GOWORK=off. There is no aggregate check target or tool version validation target.

| Command | Scope |
|---|---|
| `make modules` | List all discovered development modules. |
| `make test` | Fresh ordinary race tests across all modules. |
| `make test-integration` | Files with integration build tag; execute TestIntegration… functions only. |
| `make test-e2e` | Files with e2e build tag; execute TestE2E… functions only. |
| `make test-live` | Files with live build tag; execute TestLive… functions only, including paid calls. |
| `make lint` | Formatting diff and lint without rewriting files. |
| `make fix` | Go fix, formatting and lint fixes; modifies files. |
| `make fuzz` | Every discovered fuzz function separately, 30 seconds per function. |
| `make bench` / `make cover` | Benchmarks / per-module coverage. |

Build tags alone do not exclude ordinary test files. Profile targets combine the tag
with a matching test-name prefix so a module without such tests executes none.
Use the same convention for new tests; each profile runs directly through Go too:

```sh
GOWORK=off go test -race -tags=integration -run '^TestIntegration' ./...
GOWORK=off go test -race -tags=e2e -run '^TestE2E' ./...
```

Recipes use tools from PATH and explicitly propagate command failures.
Tool versions are pinned in CI, not enforced by Make. CI and source release gates
run lint, fresh unit tests, integration and e2e sequentially.

## Project content

The root module and eight `ext/*` adapter modules share the same automatic inventory.
`internal/infratest` is a test-only package in the root module; it adds no production
dependencies or separate publication module. It checks Make discovery, failure
propagation, exact fuzz selectors, profile selection and the Bash release protocol
against disposable bare repositories.

HTTP local-server checks and Redis miniredis checks use `integration`.
HTTP ownership, fanout and verified replay composition use `e2e`.
The consumer e2e test builds unpublished module ZIP/mod/info artifacts in a temporary
file proxy, resolves all discovered routery modules without replacements or workspace,
and builds and runs a consumer. It needs network access for third-party dependencies.
Modules without matching profile tests legitimately report no tests; HTTP/Redis
integration and HTTP/consumer e2e must execute their selected functions.

No external database, Docker daemon, Python interpreter, PDF runtime or paid service
is required by these profiles. Git, Bash, Make, Go and a C toolchain for the race
detector are required. Failed prerequisites fail the selected tests, never skip them.
The live target follows the common convention; routery currently has no live tests.

CI pins Go 1.27.2 and golangci-lint 2.14.0, caches every module's go.sum and runs
`go mod download` in every discovered module. Unlike `download all`, this does not
fetch the test dependency graph of third-party modules or require adding its sums. It runs the four gates sequentially on Linux.
The regular .golangci.yml groups routery imports and retains common lint rules;
ragy-specific type and path exclusions are removed.

Performance measurements, fuzz campaigns and paid provider calls remain separate.
Historical reports retain the commands used when their results were recorded.

## Infrastructure reference

Makefile and Bash release follow ragy commit
`91e3ff2cdc87c49b5ad9d1cf0afa23d35832521e`. Routery uses Go 1.27.2 to match the reference and its
current manifests and does not carry ragy's PDF runtime. The former Python
consumer/fuzz checks and standalone fuzz shell entrypoint are replaced by Go
contract tests and the standard Make recipes. Historical reports preserve their
original commands; use this document for current verification.

## Initial verification record — Go 1.27.1, 2026-10-09

| Check | macOS arm64 | Linux amd64 |
|---|---|---|
| actionlint 1.7.12 | PASS | PASS |
| golangci-lint 2.14.0 config verify | PASS | PASS |
| make lint | PASS, nine modules | PASS, nine modules |
| make test | PASS, nine modules | PASS, nine modules |
| make test-integration | PASS, 12 selected tests | PASS, 12 selected tests |
| make test-e2e | PASS, five selected tests | PASS, five selected tests |

Both environments used Go 1.27.1. Linux ran in `golang:1.27.1-bookworm` against
a read-only source copy, with a local proxy of previously downloaded third-party
archives and a separate Linux module cache. Dependency download completed without
changing manifests. GitHub Actions itself was not run.

The 20 release contract tests use disposable bare repositories. They verify exact
source delivery to main, exact candidate identities for root/nested tags, prepared
manifests, manifest-only candidate changes, each failed gate preventing publication,
atomic-push rejection, confirmation refusal, collision checks, module-path validation,
inspect/resume/finish and preservation of candidate/version during recovery.
Eight Make contract tests cover discovery, profile dispatch, formatting rejection,
command failure propagation and individual fuzz selectors. The same contract suite
also passed on macOS with the system Bash 3.2.57.

Additional macOS checks linted integration/e2e code with their build tags and used
Go JSON test events to confirm six HTTP integration tests, six Redis integration
tests and four HTTP e2e tests actually passed. The consumer e2e test resolved and
executed all nine library modules from prepared artifacts.

Makefile and scripts/release.sh are byte-identical to the pinned reference. CI uses
Go 1.27.2 after the subsequent toolchain update;
`go mod download` replaces `download all` to prefill declared dependencies without
fetching third-party test graphs. Routery has no PDF/Python/RAGY runtime requirement.
The linter retains common rules without new project exclusions; small equivalent
Go branches and a redundant test helper were adjusted to satisfy the reference's
exhaustiveness rules. Legacy fuzz/consumer entrypoints were removed after their
checks were moved into Go tests.

No production release, production push, local commit or module-version change was
performed. Paid/live calls and full fuzz/benchmark campaigns were not run.

## Go 1.27.2 verification record — 2026-10-09

All nine module manifests, go.work, CI, README and generated test manifests now
use Go 1.27.2. Historical evidence above retains its original toolchain version.

| Check | macOS arm64 | Linux amd64 |
|---|---|---|
| actionlint 1.7.12 | PASS | PASS |
| golangci-lint 2.14.0 config verify | PASS | PASS |
| make lint | PASS, nine modules | PASS, nine modules |
| make test | PASS, nine modules | PASS, nine modules |
| make test-integration | PASS | PASS |
| make test-e2e | PASS | PASS |

Both environments used Go 1.27.2. Linux ran in `golang:1.27.2-bookworm` against
a read-only source snapshot with GOWORK=off; dependency download completed for
all nine modules. The Make/release contract suite passed on both platforms.
The source/candidate atomic-push contract also passed separately on macOS with
system Bash 3.2.57. Local workspace module discovery passed with the updated
go.work. No production release, commit or push was performed.
