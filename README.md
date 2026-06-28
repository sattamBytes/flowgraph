# temporal-code-graph (`tcg`)

Static analysis for [Temporal](https://temporal.io) projects written in Go. It
reads your source **without running anything**, reconnects Temporal's
"connect by name" wiring into a graph, and lints for the bugs that graph reveals.

## The problem: Temporal connects by NAME, not by call

In a normal program, "who calls what" is a function call your tools can follow.
In Temporal, the control plane starts work by a **string name**, and the worker
that implements it often lives in a different package — or a different service:

```go
// control plane / API server
client.ExecuteWorkflow(ctx, opts, "OrderWorkflow", orderID)

// worker (different package, maybe a different binary)
w.RegisterWorkflow(OrderWorkflow)

func OrderWorkflow(ctx workflow.Context, orderID string) error {
    workflow.ExecuteActivity(ctx, ChargeCard, orderID)
}
```

Generic Go call-graph tools follow function calls and **dead-end at the SDK
boundary**, because the link is the string `"OrderWorkflow"`, not a call. `tcg`
understands this model and reconnects the pieces — then tells you when the
wiring is wrong.

## How it works

Built on `go/packages` + `go/types` (typed ASTs — not regex, not tree-sitter),
in two passes:

1. **Registry** — find `RegisterWorkflow` / `RegisterActivity`
   (`…WithOptions`) sites and map each registered **name → Go function symbol**,
   recording the **task queue** each worker registers on.
2. **Edges** — find invocation sites (`client.ExecuteWorkflow`,
   `workflow.ExecuteActivity` / `ExecuteChildWorkflow`, signals, …) and resolve
   each target:
   - a **function reference** → resolved to the exact symbol;
   - a **string literal** → looked up in the registry;
   - a **computed/variable string** → cannot be resolved statically, so the edge
     is marked **`unresolved`** and surfaced. It is never guessed.

SDK calls are identified by the **resolved package path** of the callee
(`go.temporal.io/sdk/client`, `…/workflow`, `…/worker`) — so your own
`ExecuteWorkflow` helper never causes a false positive.

The **JSON graph is the canonical artifact**; every other output (`check`,
`export`, `serve`, `mcp`) is built from it. The analyzer is fully usable
headless / in CI.

## Install

### Homebrew (macOS / Linux)

```bash
brew install sattamBytes/tap/tcg
```

### go install

```bash
go install github.com/sattamBytes/temporal-code-graph/cmd/tcg@latest
```

> `tcg` needs the Go toolchain available at runtime — it drives `go list`
> (`go/packages`) to type-check the project it analyzes. The Homebrew formula
> pulls `go` in as a dependency automatically.

## Usage

```bash
tcg build  ./...                       # emit the canonical JSON graph
tcg check  ./...                       # run all lint rules (CI gate; see below)
tcg check  ./... --json                # machine-readable findings
tcg export ./... --format mermaid      # docs diagram (also: --format dot)
tcg serve  ./...                       # interactive dashboard at localhost:8080
tcg serve  --graph graph.json          # replay a prebuilt graph, no source
tcg mcp    ./...                       # MCP server over stdio (for AI agents)
```

The path argument follows Go conventions (`./...`, a directory, a package
pattern), the same as `go vet`.

## Lint rules

| Rule | Severity | What it catches |
|------|----------|-----------------|
| `task-queue-mismatch` | **error** | A workflow started on queue X but registered on queue Y — it hangs forever. The headline rule. |
| `unknown-name` | **error** | A name referenced at a start/execute site that was never registered (typo / dead reference). Suggests the closest registered name. |
| `orphan` | warning | Registered but never started / executed. |
| `signal-mismatch` | warning | A signal/query is sent but no handler listens for that name. |
| `non-determinism` | warning | `time.Now`, `math/rand`, direct network/DB I/O, map-range, or `go` statements inside a workflow. |
| `missing-timeout` / `missing-retry` | warning | An activity executed with no timeout / no retry policy. |

`check` exits **non-zero** when any **error**-severity finding exists, so it
works as a CI/PR gate out of the box. Warnings do not fail the build.

### Suppressing a false finding

Add an inline directive on the offending line (or the comment-only line above
it). Bare suppresses all rules; named suppresses only those listed:

```go
client.ExecuteWorkflow(ctx, opts, dynamicName) //tcg:ignore unknown-name
```

## Dashboard (`serve`)

A single static page (Cytoscape.js) that reads only `graph.json`:

- clickable nodes that jump to the source location;
- hover an edge for task queue, retry, and timeout;
- filter by service and task queue;
- **blast-radius** mode: click a node to highlight everything upstream (what
  triggers it) and downstream (what it triggers), fading the rest;
- unresolved edges drawn dashed and red.

## MCP server (`mcp`)

Exposes the graph over the Model Context Protocol so AI coding agents can query
it. Wire it into Claude Code via `.mcp.json`:

```json
{ "mcpServers": { "tcg": { "command": "tcg", "args": ["mcp", "./..."] } } }
```

Tools: `downstream`, `upstream`, `who_starts`, `list_unresolved`, `get_graph`.
Ask your agent: *"what's downstream of ChargeCard?"*, *"which endpoints start
OrderWorkflow?"*, *"list all unresolved edges."*

## CI

```yaml
- run: go install github.com/sattamBytes/temporal-code-graph/cmd/tcg@latest
- run: tcg check ./...   # fails the PR on task-queue mismatches & unknown names
```

## Design notes & limits

- Pure static analysis — your code is never executed; no Temporal cluster needed.
- Unresolved edges and findings are **first-class and clearly labeled, never
  faked.** When `tcg` can't prove something, it says so.
- Options read from a struct literal (or a local variable assigned one in the
  same function). Options threaded through helpers across functions are a known
  limit — the affected metadata is simply absent, not invented.
- Activity timeout/retry is detected per workflow function (from
  `workflow.WithActivityOptions`), which is how Temporal actually applies it.

## Development

```bash
go test ./...   # tests run against a hermetic sample project under testdata/
```

`testdata/sample` is a deliberately buggy Temporal project (one of every planted
bug); `testdata/stubsdk` is a minimal stand-in for the Temporal SDK so tests need
no network.

## License

MIT — see [LICENSE](LICENSE).
