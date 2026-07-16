# gen-system v23.2 — generating & running a full CRUD project

## 0. Make `gen` runnable (fixing "command not found")

`gen` is a built binary, not an installed command. Pick one:

    # build to current dir, run by path
    go build -o ./gen ./generator/cmd/gen
    ./gen idea "..."

    # OR add the build dir to PATH for this shell
    export PATH="$PWD:$PATH"
    gen idea "..."

    # OR install permanently (install.sh builds to /opt/gen-system/bin)
    sudo ln -s /opt/gen-system/bin/gen        /usr/local/bin/gen
    sudo ln -s /opt/gen-system/bin/gen-server /usr/local/bin/gen-server

`/usr/local/bin` is on zsh's default PATH, so `gen`/`gen-server` then work anywhere.

## 1. The approval workflow (plan → review → run)

`gen idea` plans AND generates in one shot (no pause). To approve before code
is written, split it:

    ./gen plan "A task tracker: users create projects, each project has tasks
                with a title, status, and due date."
    #   → writes plan.generated.json   ← THIS is your review/approve point

    # open plan.generated.json, edit entities / fields / tasks as needed

    ./gen run --plan plan.generated.json --path ./myapp
    #   → generates the full backend into ./myapp

If you already have a schema and want to skip the model entirely (fully
deterministic, no AI):

    ./gen apply schema.json     # generates into ./output

## 2. What you get (v23.2)

A coherent, **compiling, runnable** in-memory CRUD backend (Go module `app`):

    go.mod
    backend/main.go                         # wires routes, serves :8080
    backend/internal/model/<entity>.go      # struct + JSON tags
    backend/internal/store/<entity>.go      # thread-safe in-memory CRUD store
    backend/internal/handler/<Entity>_handler.go       # full CRUD HTTP handlers
    backend/internal/handler/<Entity>_handler_test.go  # table test
    backend/internal/handler/handler.go     # shared writeJSON helper

No database, no Postgres setup. Handlers are real (list/create/get/update/
delete), not `// TODO` stubs.

## 3. Build, test, run

    cd ./myapp          # or ./output for `gen apply`
    go build ./...      # green
    go vet ./...        # clean
    go test ./...       # handler CRUD tests pass
    go run ./backend    # serves on http://localhost:8080

## 4. Hit it with curl

    curl localhost:8080/health
    curl -X POST localhost:8080/projects -d '{"title":"Launch","owner":"mai"}'
    curl localhost:8080/projects
    curl localhost:8080/projects/1
    curl -X PUT  localhost:8080/projects/1 -d '{"title":"Launch v2","owner":"mai"}'
    curl -X DELETE localhost:8080/projects/1

Each entity gets its own collection route (`/projects`, `/tasks`, …) and an
item route (`/projects/{id}`). IDs auto-increment from 1.

## 5. Notes / current limits

- Data is in memory: it resets when the server restarts. Real Postgres wiring
  is a separate, larger step (was Option B) — not in this build.
- A `backend/internal/validation` package is also generated and compiles, but
  the in-memory handlers don't call it yet; wiring validation into Create/Update
  is a small follow-up.
- Frontend/mobile files are still emitted by the existing generators and are
  not part of this compiling-backend guarantee.

## 6. Postgres backend (Option B) — v23.2/B

By default generation is in-memory (above). To ALSO generate a real Postgres
backend, set `GEN_BACKEND=postgres` at generation time:

    GEN_BACKEND=postgres ./gen apply schema.json
    # or: GEN_BACKEND=postgres ./gen run --plan plan.generated.json --path ./myapp

This additionally emits:

    backend/internal/db/db.go                 # opens *sql.DB from DATABASE_URL
    backend/internal/db/migrate.go            # applies embedded migrations on boot
    backend/internal/db/migrations/NNN_*.sql  # CREATE TABLE per entity
    backend/internal/store/<entity>_pg.go     # database/sql CRUD impl
    backend/internal/store/interfaces.go      # <Entity>CRUD interface
    go.mod                                     # now requires github.com/lib/pq

The generated `main.go` picks the backend AT RUNTIME via `STORE_BACKEND`
(default: memory — so nothing changes unless you ask):

    # in-memory (no DB)
    go run ./backend

    # postgres (auto-creates tables on boot)
    export DATABASE_URL="postgres://postgres:postgres@localhost:5432/app?sslmode=disable"
    STORE_BACKEND=postgres go run ./backend

Notes:
- `go test ./...` always uses the in-memory store, so tests need no database.
- Tables are auto-migrated on boot (CREATE TABLE IF NOT EXISTS) — no separate
  migrate step. The .sql files are embedded in the binary.
- Field/entity names with spaces (e.g. "due date") are sanitized to valid Go
  identifiers (DueDate) and snake_case columns/JSON (due_date) automatically.

### Verification status (honest)
- Memory mode: generated, built, tested, AND run live (curl CRUD verified).
- Postgres mode: generated and COMPILED clean (go build ./... with lib/pq),
  handler tests pass. Running against a live Postgres was not exercised in the
  build sandbox (no DB present) — the SQL is standard CREATE/INSERT/SELECT/
  UPDATE/DELETE. Verify on your box with a real DATABASE_URL.
