# flowgraph

Static **code-flow analysis for Go**. Point it at your app and it maps how a
request actually flows: from an **entrypoint** (a REST route or a Temporal
workflow) down through the functions it calls, the branches that guard them, and
— uniquely — straight across Temporal's *connect-by-name* boundary into
workflows and their activities.

It reads source with `go/packages` + `go/types` (typed ASTs — it never runs your
code, never uses regex or Tree-sitter), so it resolves which function a call
*actually* hits instead of guessing.

## Why it exists

Two blind spots in normal tooling:

1. **Generic Go call-graph tools dead-end at Temporal.** The control plane starts
   work by a **string name**, not a call:

   ```go
   // API server                         // worker (different package/service)
   client.ExecuteWorkflow(ctx, o,        w.RegisterWorkflow(OrderWorkflow)
       "OrderWorkflow", id)              func OrderWorkflow(ctx workflow.Context, id string) error {
                                             workflow.ExecuteActivity(ctx, ChargeCard, id)
                                         }
   ```
   A call follower sees a string and stops. `flowgraph` walks through it.

2. **Multi-language tools (Tree-sitter based) are imprecise** and have no REST
   route parsing, no branch flow, and no Temporal awareness. `flowgraph` trades breadth
   for Go-native precision.

## What you get

```
POST /orders → CreateOrderHandler
                 ├─ validate(req)
                 ├─ if req.Coupon != "" → applyCoupon()
                 └─ ExecuteWorkflow("OrderWorkflow")   ← jumps the Temporal name gap
                       OrderWorkflow
                         ├─ ChargeCard          (activity)
                         ├─ if charged → ReserveStock (activity)
                         └─ SendEmail           (activity)
```

- **Real call graph** — `func → func`, resolved exactly; interface/callback calls
  it can't pin are drawn dotted and labeled "unknown" (never guessed).
- **Branch context** on every call ("runs only if `err != nil`").
- **Entrypoints** auto-detected: **net/http, chi, gin, echo** (pluggable — more
  are a small resolver away), plus Temporal workflows.
- **Temporal bridge** — flows continue from a `StartWorkflow` into the workflow
  and its activities/signals/child workflows.
- **Temporal lint rules** kept from the old `tcg`: task-queue mismatch, unknown
  name, orphans, signal mismatch, non-determinism, missing timeout/retry.

## Install

```bash
brew install sattamBytes/tap/flowgraph
# or
go install github.com/sattamBytes/flowgraph/cmd/flowgraph@latest
```

> `flowgraph` needs the Go toolchain at runtime — it drives `go list` (`go/packages`)
> to type-check the project it analyzes. The Homebrew formula pulls `go` in.

## Usage

Run from your repo root (where `go build ./...` works):

```bash
flowgraph list   ./...                       # what entrypoints did it find?
flowgraph serve  ./...                       # interactive dashboard (pick an entrypoint, trace its flow)
flowgraph build  ./...                       # canonical JSON graph (everything is built from this)
flowgraph check  ./...                       # Temporal lint rules; non-zero exit on errors (CI gate)
flowgraph export ./... --format mermaid      # docs diagram (also: dot)
flowgraph mcp    ./...                       # MCP server (stdio) for AI agents
```

The path arg follows Go conventions (`./...`, a dir, a package pattern). For a
monorepo with control plane and workers in different modules, run `flowgraph` at the
common root — it loads them together so the by-name wiring resolves across
services.

## Dashboard (`serve`)

Pick an entrypoint → trace its downstream flow (handler → calls → branches →
workflow → activities); everything else collapses. Click any node for its blast
radius (callers + callees). Branch guards show on call edges; unresolved/unknown
edges are dashed-red; `HANDLES` edges (route → handler) are dotted. Reads only
`graph.json`, so it works headless too: `flowgraph serve --graph graph.json`.

## MCP (`mcp`)

Wire into Claude Code (`.mcp.json`):

```json
{ "mcpServers": { "flowgraph": { "command": "flowgraph", "args": ["mcp", "./..."] } } }
```

Tools: `list_entrypoints`-style queries via `callers`, `callees`, `downstream`,
`upstream`, `who_starts`, `list_unresolved`, `get_graph`.

## Lint rules (`check`)

`check` stays Temporal-focused and is a drop-in CI gate (exit non-zero on
errors): `task-queue-mismatch` (headline), `unknown-name`, `orphan`,
`signal-mismatch`, `non-determinism`, `missing-timeout` / `missing-retry`.
Silence a false positive inline: `//flowgraph:ignore <rule>`.

## Design notes & limits

- Pure static analysis — never executes your code.
- Unknown things are first-class: dotted edges, surfaced, never faked.
- Interface/function-value call targets aren't resolved to implementations in v1
  (shown as `InterfaceCall` nodes). A `--all-impls` expansion is a future option.
- Branch context is the *nearest* enclosing guard, not a full control-flow graph.
- gRPC and gorilla/mux resolvers are planned; adding a framework is one
  `EntrypointResolver`.

## Development

```bash
go test ./...   # hermetic: runs against testdata/sample with stub frameworks/SDK
```

`testdata/sample` is a deliberately buggy Temporal + REST app; `testdata/stub*`
are minimal stand-ins for the Temporal SDK and the HTTP frameworks so tests need
no network.

## License

MIT — see [LICENSE](LICENSE). Formerly `temporal-code-graph` / `tcg`.
