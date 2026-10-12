# site

The gen-system landing page and a static demo of the Studio, served as a Cloudflare Worker with
static assets.

| Path | What it is |
|---|---|
| `public/index.html` | the landing page, written by hand |
| `public/demo/` | the Studio page, the shim that answers its API calls from files, and the data |
| `public/demo/data/<project>/` | saved answers for `examples/payroll`, `examples/shop` and CobolCraft |
| `public/img/` | a copy of `docs/img/`, made by the build |
| `build.py` | rebuilds `public/demo/` and `public/img/` |
| `wrangler.jsonc` | the Worker configuration |

## Rebuild the demo

Run it after a change to the Studio page, the examples or the Understand engine:

```bash
python site/build.py
```

It builds `generator/cmd/studio-snapshot`, which loads each project like `understand-server`
and writes the real handlers' answers to JSON. It clones CobolCraft at the commit the benchmark
pins; pass `--cobolcraft <dir>` to use a checkout you already have. Paths in the data show as
`examples/payroll/...`, never as a local path.

In the demo, the Studio's `/api/...` requests go to `public/demo/shim.js`. It serves the fixed
pages from files and computes the call graph and search in the browser with the same rules as
`internal/presentation/v23_endpoints.go`. Verify and Correct answer 403: the demo is read only.

## Deploy

From this folder:

```bash
npx wrangler deploy
```

Or connect the repository in the Cloudflare dashboard (Workers, Create, Import a repository) with
the root directory `site` and the deploy command `npx wrangler deploy`. Then add your domain
under the Worker's Settings, Domains and Routes.

## Preview

```bash
python -m http.server 8790 --directory site/public
```

Then open http://localhost:8790/ and http://localhost:8790/demo/.
