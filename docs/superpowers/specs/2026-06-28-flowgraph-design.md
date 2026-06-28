# flowgraph — design

**Status:** approved-pending-review · **Date:** 2026-06-28 · **Binary:** `fg` · **Repo:** `flowgraph` (renamed from `temporal-code-graph`)

## 1. What it is

`flowgraph` is a **static** code-flow analyzer for **Go** applications. It reads
source with `go/packages` + `go/types` (typed ASTs — never runs code, never uses
regex/Tree-sitter), and produces a graph that traces how a request flows through
the app: from an **entrypoint** (a REST route, gRPC method, or Temporal
workflow) down through the functions it calls, the branches that guard them, and
— uniquely — **across Temporal's connect-by-name boundary** into workflows and
their activities.

It supersedes `temporal-code-graph` (`tcg`): the existing Temporal analyzer
becomes **one layer** inside a larger engine. All current behavior (Temporal
graph + lint rules + dashboard + MCP) is preserved.

### Why it's different from existing tools
- **vs generic Go call-graph tools** (go-callvis, callgraph): those dead-end at
  `client.ExecuteWorkflow("OrderWorkflow")` because the link is a string, not a
  call. `flowgraph` walks through it.
- **vs code-review-graph / graphify**: those use Tree-sitter (multi-language but
  imprecise — pattern-matches text, can't resolve Go types/interfaces), have no
  real REST route parsing, no branch flow, and no Temporal awareness.
  `flowgraph` trades breadth for **Go-native precision** + REST + Temporal.

## 2. Core algorithm

Single `go/packages` load over all module roots (so control plane and worker
resolve even across packages/modules). Then:

### 2a. Call-graph engine
Walk every function body. For each call expression, resolve the callee via
`go/types`:
- **Direct call** (`ChargeCard(x)`) or **concrete method** (`s.save()` where `s`
  has a concrete type) → **resolved** `CALLS` edge to the exact `*types.Func`.
- **Interface / function-value call** (`s.Save(x)` through an interface) → cannot
  be pinned statically → **unresolved** `CALLS` edge (dotted), surfaced not
  guessed. *(A future `--all-impls` toggle may expand these to every implementer;
  out of scope for v1.)*
- Calls into the **Temporal SDK** are handed to the Temporal layer (§2c) instead
  of producing a plain `CALLS` edge.

Each `CALLS` edge carries **branch context**: the nearest enclosing guard of the
call site — `{kind: if|else|switch-case|for|select, cond: "<source text>"}` — so
the UI can say "applyCoupon() runs only if req.Coupon != \"\"". Recursion and
cycles are handled (visited-set during any traversal; the stored graph is just
nodes+edges, traversal happens at query/render time).

### 2b. Entrypoint resolvers (pluggable)
One interface:

```go
type EntrypointResolver interface {
    // Name of the framework, e.g. "chi".
    Name() string
    // Inspect a call site; if it registers a route/handler, return an Entrypoint.
    Resolve(info *types.Info, call *ast.CallExpr) (*Entrypoint, bool)
}
```

An `Entrypoint` is `{Kind: rest|grpc|temporal, Label: "POST /orders", Method,
Path, HandlerSymbol, File, Line}`. Resolvers are matched by the **resolved
package path** of the called function (same anti-false-positive rule as Temporal).

**v1 resolvers:** `net/http` (`http.HandleFunc`, `http.Handle`, `ServeMux`),
`chi`, `gin`, `echo`. **Fast-follow:** `gorilla/mux` (its `r.HandleFunc(...)
.Methods("POST")` chain needs method extraction from the chained call), `gRPC`
(generated `RegisterXxxServer`), `fiber`. Each resolver is a self-contained file
with its own hermetic stub + testdata.

### 2c. Temporal layer (existing code)
The current two-pass analyzer (registry + edges) is preserved as a layer. Its
nodes/edges (`Workflow`, `Activity`, `Signal`, `STARTS_WORKFLOW`,
`EXECUTES_ACTIVITY`, …) merge into the same graph. A control-plane caller that
starts a workflow becomes the bridge: the enclosing function's `CALLS` chain from
an entrypoint connects to the `STARTS_WORKFLOW` edge, so tracing flows seamlessly
from `POST /orders` into `OrderWorkflow` into `ChargeCard`. Temporal **lint
rules** (task-queue-mismatch, etc.) stay exactly as they are.

## 3. Graph model (extends the current one)

**Node kinds:** `Function`, `Method`, `RESTEndpoint`, `GRPCEndpoint`,
`Workflow`, `Activity`, `Signal`, `Query`, `ControlPlaneCaller`.
Every node: `id, kind, name, symbol, file, line, service, package`.
`RESTEndpoint`/`GRPCEndpoint` also carry `method, path, handlerSymbol`.

**Edge kinds:** `CALLS` (new), plus existing `STARTS_WORKFLOW`,
`EXECUTES_ACTIVITY`, `STARTS_CHILD`, `SIGNALS`, and `HANDLES` (entrypoint →
handler function).
Every edge: `from, to, kind, resolution (resolved|unresolved|unknown), file,
line`. `CALLS` adds `branch {kind, cond}`. Temporal edges keep `taskQueue,
hasTimeout, hasRetry`.

**Entrypoints** are listed top-level in the graph for the dashboard/MCP to root on.
The **JSON graph remains the canonical artifact**; everything else is built from it.

## 4. CLI (binary `fg`)

```
fg build  <path>                       # canonical JSON graph
fg check  <path> [--json]              # Temporal lint rules (CI gate; unchanged behavior)
fg export <path> --format mermaid|dot [--entry "POST /orders"]   # whole graph or one entrypoint's flow
fg serve  <path> [--addr] [--open] [--graph graph.json]          # dashboard
fg mcp    <path> [--graph graph.json]  # MCP server
fg list   <path>                       # list detected entrypoints (quick sanity check)
```

`check` stays Temporal-focused. (Possible later general rule: "function defined
but unreachable from any entrypoint" — flagged as a future option, not v1.)

## 5. Dashboard (entrypoint-first)

- **Left:** list of entrypoints (grouped by kind: REST / gRPC / Temporal),
  searchable.
- **Pick one** → render its **rooted flow tree** downstream (handler → calls →
  branches → workflow → activities). Other entrypoints stay collapsed.
- **Click a function node** → expand its internal flow (ordered calls + branch
  guards) inline.
- Branch guards shown on edges ("if err != nil"). **Unknown edges dotted.**
  Existing Temporal styling (queues on hover, dashed unresolved) preserved.
- Reads only `graph.json`; rooting/drill-in is client-side traversal (Cytoscape).

## 6. MCP tools (extends current)

Keep `downstream/upstream/who_starts/list_unresolved/get_graph`. Add:
`list_entrypoints`, `trace_entrypoint(label)` (the rooted flow), `callers(func)`,
`callees(func)`.

## 7. Testing

Hermetic, offline (the existing `testdata/stubsdk` pattern, extended):
- Extend `testdata/sample` with a REST layer (net/http + chi handlers) whose
  handlers call internal funcs (with a branch) and start `OrderWorkflow` — so the
  full chain `route → handler → branch → workflow → activity` is asserted end to
  end.
- Each framework resolver gets a **minimal stub package** (router types/methods
  with the real package paths) + its own testdata, so resolver tests need no
  network.
- Assertions: call edges resolved vs dotted-unknown correctly; branch context
  captured; entrypoints detected per framework; the Temporal bridge connects;
  all existing Temporal/lint/export/MCP tests still pass.

## 8. Migration (rename `tcg` → `fg`)

- Rename the GitHub repo `temporal-code-graph` → `flowgraph` (GitHub keeps the
  old URL as a redirect).
- Module path → `github.com/sattamBytes/flowgraph`; binary `cmd/fg`.
- Homebrew: new formula `Formula/fg.rb` in the tap; keep `tcg.rb` as a thin alias
  or deprecate with a note. `brew install sattamBytes/tap/fg`.
- README rewritten around the general tool; Temporal becomes a highlighted
  section, not the whole story.

## 9. Build phases (for the implementation plan)

1. **Engine + rename:** general `CALLS` call-graph with resolution + branch
   context; merge with the existing Temporal layer; rename to `flowgraph`/`fg`;
   update build/export/mcp/tests. (No new entrypoints yet — Temporal workflows
   already act as entrypoints.)
2. **REST entrypoints + tracing:** resolver interface + `net/http` and `chi`;
   `HANDLES` edges; `fg list`; entrypoint-rooted dashboard tracing + function
   drill-in.
3. **More resolvers:** `gin`, `echo`, `gorilla/mux`, then `gRPC`/`fiber`.

## 10. Non-goals (v1)

- Non-Go languages (that's the Tree-sitter trade-off this tool deliberately avoids).
- Whole-program RTA/SSA interface resolution (dotted-unknown is the v1 answer).
- Full per-statement control-flow graph (we annotate calls with their guard, not
  every block).
- Runtime/dynamic analysis of any kind.
