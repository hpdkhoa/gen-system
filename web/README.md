# web

- `studio-v23.html` is the Studio. `understand-server` serves the copy embedded at
  `generator/internal/webui/studio-v23.html`; `gen-server` serves this folder from disk
  (`--web`, else `<binary dir>/web`, else `./web`). Keep the two copies identical.
- `legacy/` holds the v20 Studio (`studio.html`) and the three pane generate console
  (`generate.html`). They are development assets, still reachable from `gen-server` under
  `/legacy/`, and not part of the embedded build.
