# STUDIO_V23_GUIDE — using the redesigned Studio

The runbook for `studio-v23.html`, the v23 redesigned UI. The legacy
`studio.html` is still served so you can fall back if anything is wrong.

## Start it

```bash
go build ./generator/cmd/gen-server
./gen-server --root /home/kai-hoang/Project/gen-system/gen-system --copybooks /home/kai-hoang/Project/gen-system/cobol/copybooks
./gen-server --root /home/kai-hoang/Project/gen-system/gen-system --copybooks /home/kai-hoang/Project/gen-system/gen-system/_verify_idea_app
```

The banner prints two URLs:

```
   Studio (v23): http://localhost:28080/studio-v23.html
   Studio (v20, legacy): http://localhost:28080/studio.html
```

## Layout

Three persistent columns:

- **Left** — primary navigation (Architecture / Graph / Beliefs / Search),
  discovery shortcuts (Entry points, Effects, Unknowns), Recent history.
- **Centre** — the active page.
- **Right** — context panel for the selected symbol.

State lives in the URL fragment (`#/graph/root=...`). The URL round-trips —
paste it into a chat or issue, it lands on the same view.

## Four jobs

### 1. Orient on a codebase — Architecture

The landing page. Shows:

- Symbols indexed + edge-resolution percentage (headline reliability number)
- Language breakdown with horizontal bars
- Top packages by symbol count
- Entry points (mains, HTTP handler shapes, CICS/COBOL transactions),
  clickable to open the call graph centred on that symbol

### 2. Understand a flow — Graph view

Two coordinated panes:

- **Call graph** — layered DAG centred on the selected symbol. Depth 1–4;
  direction callees / callers / both. Node strokes colour-coded by dominant
  effect: blue=db, amber=fs, red=network, green=pure, dashed purple = unknown
  or unresolved.
- **CFG** — vertical stack of basic blocks for the current function. Entry
  blocks are green-bordered, exit blocks red-bordered. Successor edges
  listed inline (`→ B2, B3`).

Click any node in the call graph to re-root.

Caps at 200 nodes; a toast warns you. Narrow depth or direction to refocus.

### 3. Review beliefs — Belief inbox

A queue, not a list. Each row:

- Subject + predicate + object
- Confidence bar (green ≥ 90%, amber ≥ 70%, red below)
- Source kind + numeric confidence
- Accept / Correct / Defer actions

Bulk select via checkboxes. Filters: `all`, `pending`, `verified`, `low`.
Default sort: confidence ascending (riskiest first).

Corrections post to `/api/present/beliefs/correct` (v15 endpoint). The
correction store is separate from beliefs and re-applies after every
re-parse.

### 4. Search — cmd-K palette + full page

`⌘K` / `Ctrl-K` opens the global palette. Type to filter, `↑↓` to navigate,
`↵` to open, `Esc` to close. Prefixes:

- `effect:db` — only DB-effect functions
- `effect:fs`, `effect:network`, `effect:pure`

Full Search page (`g s`) is the same engine with a stable URL and horizontal
effect facets.

## Keyboard

| Key | Action |
|---|---|
| `⌘K` / `Ctrl-K` | Open palette |
| `g a` | Architecture |
| `g g` | Graph |
| `g b` | Beliefs |
| `g s` | Search |
| `[` / `]` | Prev / next in history |
| `Esc` | Close palette |

In an input field, hotkeys don't fire.

## Live updates

The connection dot turns green when SSE is open. File changes in the watched
root trigger a toast and invalidate the architecture cache so the next visit
reflects the new state.

## Backend endpoints used

| Endpoint | Used by |
|---|---|
| `GET /api/present/v23/architecture` | Architecture page, nav badges |
| `GET /api/present/v23/search?q=&effect=&limit=` | Search page, palette |
| `GET /api/present/v23/callgraph?root=&depth=&direction=` | Call-graph pane |
| `GET /api/present/v23/effects` | Effects discovery page |
| `GET /api/present/flow?func=` (v15) | CFG pane |
| `GET /api/present/beliefs` (v15) | Belief inbox |
| `POST /api/present/beliefs/correct` (v15) | Accept / Correct |
| `GET /api/events` (SSE) | Live dot, toasts |

## Offline / no-CDN posture

The only network asset is Google Fonts (cosmetic — page renders with system
fallbacks if blocked). No JavaScript runtime, no build step, no bundler. The
page is one HTML file. Read it, change it — that's the workflow.

## Known limitations (v23.0, deferred to v23.1)

- Architecture page doesn't aggregate `NodeUnknown` counts across files.
  Unknowns page falls back to empty state.
- Belief Correct uses `prompt()`. Inline edit lands in v23.1.
- CFG pane is a vertical stack, not orthogonal 2D layout.
- `/api/present/diff` exists but not yet surfaced.

## Reporting an issue

1. Check the connection dot — grey means SSE isn't connected.
2. Open the browser console — every fetch failure is logged.
3. Hit the backend endpoint directly with curl.
4. If v15 endpoints work but v23 doesn't, the bug is in `v23_endpoints.go`,
   not your data.
