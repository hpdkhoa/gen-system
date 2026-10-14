# Security policy

## Supported versions

| Part | Version that gets fixes |
|---|---|
| gen-system, the Go code | the latest commit on `main` |
| understand-mcp (npm `gen-understand`) | the latest 0.2.x release |

Older releases get no fixes. Upgrade to the latest release first and check that the problem is
still there.

## Report a vulnerability

Do not open a public issue for a security problem.

Report it privately through GitHub: open the repository's **Security** tab and click
**Report a vulnerability**. If that does not work for you, email contact@500kv.co.

Include:

- the version or commit;
- what an attacker can do, and what access they need first;
- the steps or the smallest input that shows it.

I reply within 7 days. When I confirm the problem, I fix it, publish a release and a GitHub
security advisory, and credit you in the advisory unless you ask me not to. Please give me 90
days, or until the fix ships, before you publish the details.

## What gen-system trusts

These are design limits, not bugs. Reports about them are still welcome if you see a way to
reduce the risk.

- **The web servers have no login.** `gen-server` and `understand-server` listen on every network
  interface on the port you give them, and anyone who reaches that port can read the parsed code.
  Run them on your own machine or behind a firewall. Do not expose them to the internet.
- **The MCP server talks over stdio only.** It opens no network port. It reads the project root it
  starts on, plus the copybook and standard library folders you name, and refuses a file outside
  the project root. It writes nothing unless you start it with `--allow-corrections`; then the
  `correct_belief` tool writes `.gen/beliefs.json` in the project.
- **The Python parser runs `python3`.** It runs the standard `ast` module in a subprocess and does
  not import or run the files it parses.
- **Generated code is untrusted until you review it.** The generation half writes code with a
  local model. The model's security review warns and never blocks. Read the code before you run
  it anywhere that matters.
- **The models run locally through Ollama.** gen-system sends your code to no outside service.

## In scope

- Code execution, file reads outside the project root, or path traversal through any parser,
  the MCP server or the web servers.
- A crafted source file that crashes or hangs a parser for a long time.
- A crafted release or install path: `install.sh`, the npm package, the release workflow.

## Out of scope

- Problems that need access to the machine or the open server port already.
- Bugs in Go, Node, Python or Ollama themselves. Report those upstream.
- The static demo at gen-system.500kv.co, unless it serves something harmful.
