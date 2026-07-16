# gen-system — Operations, Workflows & UX Guide

*Version baseline: v23.8 (engine) / v23.x Studio. Last updated for the v23.8 session.*

This is the practical manual: how to build it, how to run it, every command it
exposes, the workflows that string those commands together, and a concrete plan
for making the product something a user wants to stay inside of.

Everything here is taken from the actual source tree (`generator/cmd/*`,
`web/studio-v23.html`, the install/build scripts), not from memory. Where a
behaviour is verified-on-box vs verified-in-sandbox, it says so.

---

## 1. What gen-system is, in one paragraph

gen-system is a **local-first, offline** system that does two things from the
same code-intelligence core:

1. **Generation** — turns a plain-language goal or a JSON schema into a runnable
   product (Go backend today as the compile-guaranteed layer; React web, React
   Native, Postgres, TypeScript types, Docker as additional layers).
2. **Understanding** — parses an *existing* codebase (Go, TypeScript, Python,
   CA Gen, COBOL/CICS) at compiler accuracy: resolves symbols exactly, builds
   call graphs and control-flow graphs, infers side effects, and exposes every
   inference for a human to confirm before the system acts.

The whole thing runs against a **local Ollama model** (default
`deepseek-coder-v2:16b-lite-instruct-q5_K_M`), with deterministic decoding
(temp 0, seed 42). No cloud calls. No telemetry. This offline + deterministic +
human-in-the-loop posture is the product's spine — keep it in mind, because it's
also the differentiator (see the companion analysis doc).

---

## 2. The three binaries

| Binary | What it is | You run it… |
|---|---|---|
| `gen` | The CLI. Project lifecycle, planning, generation, migrations, skill files. | From a project directory. |
| `gen-server` | The Studio: web UI + JSON API on `:28080`. The demo surface. | Pointed at a project root. |
| `genbench` | The benchmark harness. Measures resolution/CFG/effects/diff/gen/determinism. | For producing the one number a design partner or VC will ask for. |

All three build from `generator/cmd/{gen,gen-server,genbench}`.

---

## 3. Build

The repo ships `build.sh`, which is the supported path.

```bash
# from the repo root (the dir with go.mod)
./build.sh                  # builds gen-server + gen, runs go vet
./build.sh --run            # build, then start the server on :28080
./build.sh --with-bench     # also build genbench
./build.sh --test           # also run the test suite
./build.sh --clean          # remove built binaries first
```

`--run` accepts the same flags you'd pass the server:

```bash
./build.sh --run --root ./myproject --port 28080 \
           --copybooks ./copybooks --browse-root ..
```

If you'd rather build directly:

```bash
go build ./generator/cmd/gen
go build ./generator/cmd/gen-server
go build ./generator/cmd/genbench
go vet  ./generator/...
```

> **`gen: command not found`** is the #1 first-run snag. The binaries are not on
> your `PATH` by default. Either call them by path (`./gen ...`), copy them onto
> the path (`sudo cp gen gen-server /usr/local/bin/`), or build with
> `go build -o /usr/local/bin/gen ./generator/cmd/gen`.

Go 1.25.0 is the declared toolchain; a local 1.22 build works for development if
you relax the `go` directive in a throwaway copy (the shipped `go.mod` stays
1.25). The single external dependency is `github.com/lib/pq`.

---

## 4. Install (server / appliance)

For a fresh Linux box, `install.sh` does the whole appliance setup: system
packages, Go, Ollama, the default model pull, optional PostgreSQL 15, builds the
binaries, installs to `/opt/gen-system`, and creates a service user.

```bash
sudo ./install.sh                  # full install
sudo ./install.sh --no-postgres    # skip Postgres (using an external DB)
sudo ./install.sh --no-ollama      # skip the LLM runtime (offline reinstall)
sudo ./install.sh --skip-model     # don't pull the model now; do it later
sudo ./install.sh --model=NAME     # pull a different primary model
sudo ./install.sh --extra-models=A,B   # pull additional models in a loop
sudo ./install.sh --prefix=/srv/gen    # custom install prefix
```

Tested on Ubuntu 22.04 / 24.04 and Debian 12 (dnf/yum fallback exists for other
distros). The engine reads `OLLAMA_MODEL` at runtime, so you can repoint the
model without rebuilding.

`install-service.sh` registers the systemd unit; `restart.sh` reinstalls without
re-pulling the model and restarts the service.

---

## 5. The `gen` CLI — every command

Running `gen` with no arguments prints usage. Here is the full surface, grouped
the way the CLI groups it.

### Planning (with a human review pause)

```bash
gen plan "build a blog with posts and comments"
#   → writes plan.generated.json for you to read/edit before anything runs

gen run --plan plan.generated.json --path ./myapp
#   → executes the reviewed plan through the agent loop
```

This is the **approve-before-build** path. You see the plan as JSON, edit it
(rename entities, drop a field, fix a relation), then run it. This is the path to
demo, because the review step is the product's whole thesis.

### Full pipeline (no review pause)

```bash
gen idea "a task tracker with projects and tasks"
#   → idea → plan → schema → generate, in one shot, into the current directory
```

`idea` is the fast path. It still writes `plan.raw.json` / `schema.raw.json`
along the way (so if the model emits junk, you can inspect exactly what it
produced), but it does not stop for you.

### Schema-based generation

```bash
gen init                          # initialize project directories
gen apply schema.json             # generate the full stack from a schema
gen evolve <path> --schema file   # incrementally evolve an existing project
gen schema plan.json              # convert a plan JSON into a schema JSON
```

`apply` is the most deterministic entry point — no model needed if you hand it a
schema. It's the right thing to run in CI or when you want reproducible output.

A schema looks like this (relations are first-class as of the FK work in v23.8):

```json
{
  "entities": [
    {"name": "User", "fields": [
      {"name": "username", "type": "string"},
      {"name": "email", "type": "string"}
    ]},
    {"name": "Post", "fields": [
      {"name": "title", "type": "string"},
      {"name": "body", "type": "string"}
    ], "relations": [
      {"type": "belongsTo", "target": "User", "foreignKey": "AuthorID"}
    ]}
  ]
}
```

### Database

```bash
gen migrate schema.json            # apply DB migrations
gen migrate schema.json --dry      # show what would change, change nothing
gen migrate schema.json --force    # apply without the safety prompt
```

### Intelligence

```bash
gen skill --root . --out .gen/project.skill --constraints constraints.txt
#   → writes a versioned .skill file: a portable snapshot of the symbol graph
#     for this project that other tools (or a future session) can load.
```

### Utilities

```bash
gen deps <path> stable             # write dependency files, pinned/stable profile
gen deps <path> latest             # …latest profile
gen doctor                         # check project health
gen ingest                         # diagnostics ingest
gen finetune                       # fine-tune pipeline entry (internal/finetune)
```

---

## 6. The generation workflow, end to end

This is what actually happens when you go from an idea to a running server. The
agent executes a task list; the task types in the executor are:
`generate_model`, `generate_repo`, `generate_handler`, `generate_validation`,
`generate_service`, `generate_router`, `generate_types`, `generate_types_barrel`,
`generate_frontend`, `evolve_frontend`, `generate_mobile`, `generate_auth`,
`generate_env`, `generate_worker`, `migrate`, `fix`, and `test`.

```
plain-language goal
   │  gen plan / gen idea
   ▼
plan.generated.json  ──(you review/edit; gen idea skips this)
   │  gen run / gen idea
   ▼
schema (entities + fields + relations)
   │  agent task loop
   ▼
generated app:
   backend/main.go
   backend/internal/model/<entity>.go        (struct + JSON tags; FK from belongsTo)
   backend/internal/store/<entity>.go         (in-memory CRUD, thread-safe)
   backend/internal/store/<entity>_pg.go       (Postgres CRUD, when GEN_BACKEND=postgres)
   backend/internal/store/interfaces.go        (<Entity>CRUD interface)
   backend/internal/handler/<Entity>_handler.go (full CRUD HTTP + validation calls)
   backend/internal/validation/<Entity>_validation.go (typed Validate<Entity>)
   backend/internal/db/* + migrations/*.sql    (Postgres mode only)
   go.mod
   │  internal validate loop: go vet + go build (up to 3 rounds, self-heals)
   ▼
"Agent complete: ✅ N done ❌ 0 failed"
```

### Running and hitting the generated app

The generated Go backend is the compile-guaranteed layer:

```bash
cd <generated-app>            # often ./output if you didn't pass --path
go build ./...                # builds clean
go test  ./...                # the generated handler tests pass
go run ./backend              # listens on :8080

# CRUD over the in-memory backend:
curl -X POST localhost:8080/posts -d '{"title":"a","body":"b"}'   # 201
curl localhost:8080/posts                                         # 200, list
curl localhost:8080/posts/1                                       # 200, one
curl -X PUT localhost:8080/posts/1 -d '{"title":"a2","body":"b"}' # 200
curl -X DELETE localhost:8080/posts/1                             # 204
curl localhost:8080/health                                        # ok
```

Validation is live (v23.8): a POST/PUT missing a required field returns `400`
with a message like `body is required`. Foreign keys from `belongsTo` relations
appear as columns in both the Go model and the Postgres migrations.

### Switching to Postgres

Generate with `GEN_BACKEND=postgres` set, and the app gains a dual-mode `main.go`
that selects the store at runtime:

```bash
GEN_BACKEND=postgres gen apply schema.json
# then, to run against Postgres instead of memory:
DATABASE_URL=postgres://user:pass@localhost:5432/app?sslmode=disable \
STORE_BACKEND=postgres go run ./backend
```

Migrations are embedded and auto-applied on boot (`CREATE TABLE IF NOT EXISTS`),
with FK constraints added in a trailing, idempotent migration so table order
never breaks the apply. *Note: the SQL path is verified by build + standard SQL
review; running against a live Postgres is a verify-on-box step.*

---

## 7. The understanding workflow (Studio)

This is the half of the product that has no real open-source equivalent for the
legacy languages, and it's the half you demo.

```bash
gen-server --root /path/to/project --port 28080
# for COBOL:
gen-server --root /path/to/cobol-project --copybooks /path/to/copybooks \
           --browse-root /path/to/parent
```

Then open `http://localhost:28080/studio-v23.html`. The Studio is a single
vanilla-JS HTML file (no build, no SPA bundler) with **URL-hash routing**, so
every view is a shareable link.

### The four pages and their keyboard shortcuts

| Page | Shortcut | What it shows |
|---|---|---|
| **Architecture** | `g a` | The high-level map: resolution %, entry points, modules. The opening shot of a demo. |
| **Graph** | `g g` | Call graph + control-flow graph, coordinated. Pick a symbol → see who calls it / what it calls, then click a function to drop into its CFG. |
| **Beliefs** | `g b` | The inference inbox: every uncertain inference the system made, with bulk accept/correct. This is the human-in-the-loop surface. |
| **Search** | `g s` | Symbol search with facets and URL state. |
| Command palette | `⌘K` | Jump to any symbol by name (subsequence match). |

The Graph view is where the recent engineering went: arrowheads with direction
(grey = calls, amber = called-by, purple dashed = unresolved), a tall vertical
Sugiyama layout that fits-to-view, and a CFG that renders **real source lines**
(not a flattened "call X" list) with branch/loop blocks split out and distinct
else-blocks. For a Go function with `if/else` where both branches return, the CFG
now shows clean entry → then/else with no orphan merge block (v23.8).

### The demo tour (the one that lands)

1. `g a` — "Here's a legacy codebase it has never seen. 94% of symbols resolved
   exactly. Here are the transaction entry points."
2. Click a transaction → `g g` — "Call graph rooted on it. Every edge is a real,
   resolved call. The amber ones are who calls *into* here."
3. Click any function — "Its control flow, from the actual source, with the
   branches split. No LLM guessing — this is compiler-accurate."
4. `g b` — "Everything it *wasn't* sure about is here for a human to confirm
   before the system changes a line. That's the trust model."

---

## 8. genbench — getting the one number

A design partner or a VC will ask "how good is it, in a number." `genbench`
exists to answer that. Subcommands:

```bash
genbench resolution     # symbol resolution accuracy
genbench cfg            # CFG construction quality
genbench effects        # effect-inference accuracy
genbench diff           # schema diff correctness
genbench parse          # parse coverage / speed
genbench loadctx        # context-load timing
genbench incremental    # incremental re-index timing
genbench gen            # generation success
genbench determinism    # same input → same output (the offline promise)
genbench model          # model-level checks
genbench regression     # regression suite
genbench diag           # diagnostics
genbench all            # everything
```

The action item that matters: **run `genbench all` against a real legacy
codebase and freeze the baseline JSON.** A resolution % and a determinism pass on
a genuine COBOL or CA Gen system is the single most fundable artifact this product
can produce.

---

## 9. How a user interacts with the product (the three modes)

There are three distinct "front doors," and a good UX makes the right one
obvious for the person standing at it:

1. **CLI-first (developer):** `gen idea` / `gen apply` / `gen run`. Fast,
   scriptable, CI-friendly. The deterministic `apply` path is the one to lean on.
2. **Studio-first (architect / reviewer):** point `gen-server` at a codebase and
   explore. This is reading and confirming, not typing.
3. **Appliance (org):** `install.sh` onto a box inside the firewall, everyone
   points their browser at it. This is the enterprise story — the offline model
   is the selling point, not a limitation.

---

## 10. Making the UX better — and making people stay

The product is technically strong and emotionally cold. Most of the
"stay-longer" wins are about **closing loops, showing progress, and rewarding
the human's attention** — not new compiler features. Concretely:

### A. Close the loop between Understand and Act
Right now Understand (Studio) and Act (CLI) are separate doors. The moment a user
can sit in the Beliefs inbox, correct an inference, and click **"apply this
correction and re-plan"** — without dropping to a terminal — the product becomes
a place you *work*, not two tools you alternate between. The belief-correction
store already exists; surface an action button on it.

### B. Make the first 5 minutes produce a "wow"
The first run should end with something running, not a wall of file paths. After
`gen idea`, auto-offer: *"Your app is built and passing tests. Start it now? [y]"*
then print the live `curl` that returns `201`. Seeing their idea answer an HTTP
request in under five minutes is the retention hook.

### C. Show the resolution number everywhere, proudly
The Architecture page should open with a single big stat: **"94% resolved,
exactly."** That number is the product's credibility in one glance, and it's the
thing that makes an architect trust the call graph beneath it. Animate it
counting up on load — it's earned.

### D. Progress and momentum during long runs
The agent loop can run for a while. A live task feed in the Studio (it already
publishes over SSE) — "generate_model ✓ … generate_handler ✓ … validate round 1
✓" — turns dead waiting into watchable progress. People stay for a progress bar
that's clearly moving.

### E. Persistent sessions and "pick up where you left off"
A project remembers its last plan, last beliefs reviewed, last graph view. On
reopen: *"Last time you were reviewing the PAYROLL transaction's call graph —
jump back?"* Continuity is what turns a tool into a workspace.

### F. Make corrections feel rewarding, not like data entry
Every belief a human corrects makes the next inference better. Say so: *"You've
corrected 12 inferences. Resolution on this module went 91% → 96%."* Visible
compounding value is why people keep curating.

### G. Reduce the cold-start cliff for legacy users
COBOL/CA Gen shops are not `npm`-native. The appliance install + a browser URL is
already the right shape; finish it with a one-page printed runbook (literally a
PDF), a sample codebase preloaded so the Studio is never empty on first open, and
zero terminal required to explore.

### H. Keyboard-driven, link-shareable — lean into it
The `g a` / `g g` / `g b` / `⌘K` + URL-hash design is genuinely good and rare in
this niche. Put a discoverable shortcut overlay behind `?`, and make every view's
URL copy-to-clipboard with one click so an architect can paste "look at this call
graph" into a ticket. Shareable state is what makes a tool spread inside a team.

### The honest framing
None of the above is a moat — they're table stakes for stickiness. The moat is
the offline, deterministic, compiler-accurate legacy understanding. The UX work
exists to make sure that moat is *experienced*, not buried behind two separate
command-line tools.

---

## 11. Current limits (be honest with users)

- **Handlers for the generated app are full CRUD; business logic is yours.** The
  generator guarantees a compiling, tested CRUD skeleton, not your domain rules.
- **The Go backend is the compile guarantee.** Web/mobile/Docker layers are
  generated but not part of the build-green promise.
- **Postgres mode is build- and SQL-verified, not yet live-DB-verified** in this
  environment. Run it against a real Postgres before depending on it.
- **The full idea→code→studio chain has only been verified against a live model
  on a box with Ollama running.** Everything in the sandbox stops at the planner
  because no model is present. This is the one real confidence gap; it's a
  you-on-your-box check, not a code defect.
- **Two pre-existing COBOL frontend test failures** (CICS verb ordering, COPY
  REPLACING) are known and tracked; they predate the current work.

---

## 12. Quick reference card

```bash
# build + run the studio
./build.sh --run --root ./myproject

# fastest path: idea to running app
gen idea "a task tracker with projects and tasks"
cd output && go run ./backend          # then curl localhost:8080/...

# reproducible path: schema to app
gen apply schema.json

# review-first path
gen plan "..."        # edit plan.generated.json
gen run --plan plan.generated.json --path ./app

# understand an existing codebase
gen-server --root /legacy/project --copybooks /legacy/copybooks
# open http://localhost:28080/studio-v23.html   (g a / g g / g b / g s / ⌘K)

# the fundable number
genbench all           # freeze the baseline JSON

# portable context snapshot
gen skill --root . --out .gen/project.skill
```
