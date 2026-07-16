# gen-system — Usage Manual (v23.10)

*How to put in your idea and get a working product out. Covers the web UI, the
interactive CLI, the generated business logic, and building a shippable artifact.*

This manual is about the **GENERATE** side (your internal build tool). The
**UNDERSTAND** side (the enterprise Studio for reading legacy code) has its own
flow — see the Operations guide. Here the goal is: *idea → running product*.

---

## 1. The short answer: where do I put my idea?

You have **two front doors**, and they take the same idea text:

| | Web UI (recommended) | Interactive CLI |
|---|---|---|
| Where | `http://localhost:28080/generate.html` | your terminal |
| Feels like | an editor with a prompt box | one command |
| Idea input | a large textarea (placeholder pre-filled) | a quoted string |
| Draft saved? | **yes — cached in your browser as you type** | no |
| Live progress | yes (plan → generate → validate, streamed) | yes (printed) |
| Best for | writing/iterating on the idea | scripts, CI, repeatability |

If you "prefer a UI like an editor where the prompt is a placeholder you fill,
saved in cache" — that is exactly **generate.html**. Read §3.

---

## 2. Start the server

The web UI is served by `gen-server` (the internal all-in-one). Run it from the
repo root (so it can find the `web/` folder):

```bash
./build.sh --internal          # builds gen, gen-server, understand-server
./gen-server --root . --port 28080
# then open http://localhost:28080/generate.html
```

For real business logic from your idea you need a local model running (see §5).
Without one, generation still works and produces a complete app — using the
built-in deterministic logic templates instead of model-written logic.

---

## 3. The web UI (editor-style, draft-cached)

Open `http://localhost:28080/generate.html`. You'll see:

- **A big prompt box** with a placeholder showing the shape of a good idea:
  *"A task tracker: users create projects, each project has tasks with a title,
  status, and due date."* Click in and replace it with your own.
- **Draft auto-save.** As you type, your idea is saved in the browser
  (localStorage). A small "Draft saved" note flashes. If you refresh, close the
  tab, or come back tomorrow, your text is **restored automatically** ("Restored
  your last draft"). Nothing is sent anywhere until you press Generate — the
  draft is purely local to your machine.
- **Clear** wipes the box and the cached draft.
- **Generate** (or ⌘/Ctrl+Enter) starts the build. The draft cache is cleared
  once the build is accepted, so your next visit starts fresh.

After you press Generate, the page shows a **live phase tracker** — Plan →
Generate → Validate — and a streaming feed of each step (`generate_model ✓`,
`generate_handler ✓`, `validate round 1 ✓`, …) over SSE. When it finishes you
get a build-green app on disk.

### What makes a good idea (this directly affects output quality)
Be concrete about **entities and their fields**, and name the **operations** you
want beyond CRUD. The model turns your sentences into both the data model *and*
the business logic, so:

> *"An orders app. An Order has a total and a status. Customers can **check out**
> an order (only if it has a total; mark it paid) and **apply a discount**
> percentage to an unpaid order. A Product has a name and stock; staff can
> **adjust stock** up or down but it can never go negative."*

The verbs in **bold** become real service methods (`Checkout`, `ApplyDiscount`,
`AdjustStock`) with the rules you described — not just CRUD.

---

## 4. The CLI (scriptable, same idea text)

Three entry points, fastest to most controlled:

```bash
# fastest: idea → plan → schema → generate, one shot.
# Output goes to ./generated-app by default (NEVER your cwd/source). Use --out
# to choose. Run it from OUTSIDE the gen-system source tree, or pass --out to a
# folder outside it — generating into the source is refused on purpose.
gen idea --out ~/apps/orders "an orders app where customers check out orders and
          apply discounts; products have stock that staff adjust, can't go negative"

# review-first: see the plan as JSON, edit it, then run
gen plan "an orders app ..."                 # writes plan.generated.json
#   …edit plan.generated.json (rename entities, fix fields)…
gen run --plan plan.generated.json --out ~/apps/orders

# reproducible: hand it a schema directly (no model needed)
gen apply schema.json --out ~/apps/orders    # default ./output
```

`gen idea` is the CLI equivalent of typing in the web box and pressing Generate.
The web UI calls the same pipeline underneath.

### 4.1 Output-directory safety (read this once)

The generator **never assumes where to write**, and it keeps generated output
separate from your source so you can review before trusting it:

- **`--out <dir>`** on `idea`, `apply`, `run`, `plan` chooses the output folder.
  Defaults: `idea` → `./generated-app`, `apply`/`run` → `./output`. The default
  is announced every time so you always know where files landed.
- **The gen-system source tree is off-limits.** If `--out` (or the default)
  resolves into the source checkout (a `module gen-system` go.mod above it, or a
  `generator/internal/engine` dir), generation is **refused** with a clear
  message. This is what stops `gen idea` run from inside the repo from dumping an
  app into your codebase.
- **No overwrite without notice.** If the output dir already holds a generated
  app (`backend/main.go` etc.), you get a `[y/N]` prompt. `--force` (or `--yes`
  for non-interactive/CI) overwrites deliberately; anything else aborts.
- **Evolving existing code is staged, not clobbered.** `gen evolve <path>
  --schema f` writes into **`<path>.evolve-review`** (a copy of your project) by
  default and leaves your real source untouched. Inspect it:
  ```bash
  gen evolve ./myapp --schema new.json          # stages to ./myapp.evolve-review
  diff -ru ./myapp ./myapp.evolve-review         # review the change
  gen evolve ./myapp --schema new.json --apply   # write in place (asks to confirm)
  ```
  `--review-dir <d>` overrides where staging goes; `--apply` writes in place (with
  a confirmation unless `--force`/`--yes`).
- **Read-only commands are safe by construction.** `gen-server --root`,
  `--copybooks`, and `--browse-root` only *index* code for the Understand/Studio
  tab — they never write generated files, so they need no output flags.

---

## 5. Getting REAL business logic (the model step)

The business logic in the generated service layer is written by a **local Ollama
model** on your box. This is opt-in:

```bash
# pull the model once (the installer does this for you):
ollama pull deepseek-coder

# generate WITH live model-written logic:
GEN_LOGIC=ollama gen idea "an orders app with checkout and discounts"
```

- **Default (no `GEN_LOGIC`)**: deterministic, offline — the engine fills the
  service layer from built-in templates. Always works, always build-green, no
  model required. Great for trying the flow.
- **`GEN_LOGIC=ollama`**: the model reads your idea text + the resolved schema
  and writes the method bodies. This is where "describe it and get real logic"
  happens.

Either way, the safety net is the same: a method the model writes that doesn't
compile is automatically dropped to a `"not implemented"` stub and the rest of
the app still builds. You never get a broken product — worst case is a clearly
marked gap.

> **One honest caveat.** Whether the model writes *good* logic for your specific
> idea depends on the model. The machinery (prompt → parse → inject → compile →
> test) is solid and tested; the quality of a 16B local model's output is the
> part to evaluate on your own box. Read each generated `*_service.go` before you
> rely on it.

### 5.1 Use the right model for each phase (recommended)

The pipeline does three different jobs, and they want different models:

1. **Analyse idea → features, design architecture, propose entities** — a
   *reasoning / instruction-following* job, not a coding job.
2. **Write the service-logic bodies** — a *coding* job.

A coder model doing job 1 is part of why early runs over-generated entities. You
can now point each phase at its own model:

```bash
# Planning (idea analysis + architecture + entity proposal):
export OLLAMA_MODEL_PLAN=qwen3:14b
# Code (service-logic bodies):
export OLLAMA_MODEL_CODE=qwen2.5-coder:14b
GEN_LOGIC=ollama gen idea --out ~/apps/foo "your idea"
```

Resolution is layered and back-compatible:
- `OLLAMA_MODEL_PLAN` → else `OLLAMA_MODEL` → else `deepseek-coder` (planning)
- `OLLAMA_MODEL_CODE` → else `OLLAMA_MODEL` → else `deepseek-coder` (code)

So if you set only the old `OLLAMA_MODEL`, every phase uses it exactly as before —
nothing changes. Set the two role vars to get the split.

**Hardware note (16GB VRAM, e.g. RTX 4060 Ti):** dense 14B models at Q4/Q5 are
the comfortable ceiling. `qwen3:14b` (planning) and `qwen2.5-coder:14b` (code)
both fit; Ollama loads/unloads them as phases run, so you never need both resident
at once. Avoid 30B-A3B MoE models on 16GB — they spill past VRAM and lose the
speed advantage.

---

## 6. What you get out

For an idea with `Order` and `Product`, generation produces:

```
output/
  backend/
    main.go                                  # wires shared stores + routes
    internal/model/order.go, product.go      # structs + JSON tags (FKs from relations)
    internal/store/*.go                       # thread-safe in-memory CRUD (+ _pg.go in pg mode)
    internal/handler/Order_handler.go         # CRUD: GET/POST/PUT/DELETE /orders
    internal/handler/Order_service_handler.go # business ops: /orders-ops/{id}/{op}
    internal/service/order_service.go         # REAL logic: Checkout, ApplyDiscount
    internal/service/order_service_test.go    # contract test (no panic)
    internal/validation/Order_validation.go   # required-field checks, wired into CRUD
  go.mod
  build-all.sh   bundle.sh   VERSION   .env.example   deploy/
```

Run it:

```bash
cd output && go run ./backend           # listens on :8080
curl -X POST localhost:8080/orders -d '{"total":100,"status":"open"}'   # 201
curl -X POST localhost:8080/orders-ops/1/discount -d '{"percent":10}'   # total → 90
curl -X POST localhost:8080/orders-ops/1/checkout                       # status → paid
curl -X POST localhost:8080/orders-ops/1/checkout                       # 400 already paid
```

CRUD lives under `/<entity>s`; business operations live under
`/<entity>s-ops/{id}/{op}` (a distinct prefix so they never clash).

---

## 7. Build a shippable artifact (no deploy)

Every generated project ships with build + bundle scripts. They produce a
**tarball you can copy to a server** — they do **not** deploy anything (deploy is
a separate future phase).

```bash
cd output
./build-all.sh                 # backend → static linux/amd64 binary in ./dist/
                               # web/mobile built too IF those layers exist
./bundle.sh --version 1.0.0    # → app-1.0.0-linux-amd64.tar.gz
```

The tarball contains: the static server binary, any web build + mobile bundle,
the SQL migrations, `.env.example`, and a `deploy/` folder with a systemd unit
and a `run.sh`. The binary is statically linked, so it runs on a bare Ubuntu
24.04 box (and an air-gapped one) with no dependencies to fetch.

Unpack and run anywhere:

```bash
tar -xzf app-1.0.0-linux-amd64.tar.gz
cd app && ./deploy/run.sh        # or install deploy/app.service with systemd
```

---

## 8. Layers and their guarantees (be clear-eyed)

| Layer | Generated? | Compile-guaranteed? | Business logic? |
|---|---|---|---|
| Go backend | yes | **yes** (go vet + go build, self-healed) | **yes** (service layer) |
| PostgreSQL | yes (schema + migrations + FKs) | SQL-verified; live-DB = on your box | n/a |
| ReactJS web | yes (scaffolding) | no (experimental this round) | CRUD scaffolding only |
| React Native mobile | yes (scaffolding) | no (experimental this round) | CRUD scaffolding only |

The **backend is the real product** today: compiling, tested, with the business
logic from your idea. Web and mobile are generated as scaffolding and flagged
experimental — their build scripts are written but run on your box (they need
npm/node). Don't promise the web/mobile layers to anyone until you've built them
on a real machine.

---

## 9. Quick reference

```bash
# UI: editor-style idea box, draft cached in browser
./gen-server --root . --port 28080      →  http://localhost:28080/generate.html
#   (--root/--copybooks/--browse-root are READ-ONLY: index only, never write)

# CLI: same idea, scriptable. ALWAYS choose --out (or accept the announced
# default) — generation refuses to write into the gen-system source tree.
gen idea --out ~/apps/foo "describe your product: entities, fields, operations"
GEN_LOGIC=ollama gen idea --out ~/apps/foo "..."   # real model-written logic

# review-first / reproducible
gen plan "..."   then   gen run --plan plan.generated.json --out ~/apps/foo
gen apply schema.json --out ~/apps/foo             # default ./output
#   re-running into a populated dir prompts; --force / --yes to overwrite

# evolve an existing app: staged for review by default, source untouched
gen evolve ~/apps/foo --schema new.json            # → ~/apps/foo.evolve-review
diff -ru ~/apps/foo ~/apps/foo.evolve-review        # inspect
gen evolve ~/apps/foo --schema new.json --apply     # write in place (confirms)

# run + exercise
cd ~/apps/foo && go run ./backend
curl localhost:8080/orders   ·   curl -X POST localhost:8080/orders-ops/1/checkout

# ship an artifact (no deploy)
cd ~/apps/foo && ./build-all.sh && ./bundle.sh --version 1.0.0
```

---

## 10. Troubleshooting: "it's stuck at Generate / Studio is empty"

**Stuck on the Generate step, no progress.** The planner calls your local model
several times — that's the slowest part of a build. Two things to check:

1. **Is the right model selected?** The default is the small `deepseek-coder`,
   which is fast but writes weak schemas (sometimes 0 entities → the build fails
   with a clear message). Point gen-system at the larger model you have:
   ```bash
   OLLAMA_MODEL=deepseek-coder-v2:16b-lite-instruct-q5_K_M ./gen-server --root .
   # or for the CLI:
   OLLAMA_MODEL=deepseek-coder-v2:16b-lite-instruct-q5_K_M GEN_LOGIC=ollama gen idea "..."
   ```
2. **First build after starting Ollama is slow.** The first model call loads the
   model into RAM — an 11 GB model can take 30–90s before step 1 even returns.
   The UI now streams "Step 1/5 … Step 5/5" and a cold-load notice so you can see
   it working. Subsequent builds are much faster (the model stays warm).

The server reaching Ollama is **not** a browser/port issue — gen-server runs on
the same machine and calls `http://localhost:11434` itself. If `ollama list`
shows your model and `ollama serve` is up, it will connect.

**"Build complete" but the Studio is empty.** The Studio indexes the directory
you started the server with (`--root`). The generated app lands in the output
path shown in the result panel — point a Studio at *that*:
```bash
gen-server --root /path/shown/in/the/result/panel
# then open studio-v23.html — you'll see the generated code's graph
```
A quick way to keep them aligned: start `gen-server --root .` from an empty dir,
and the generated app appears right there for the Studio to index.
