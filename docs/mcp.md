# The Understand half over MCP

`understand-mcp` gives an AI agent gen-system's facts about a codebase: the architecture, symbols,
call graphs, control flow graphs, effects, source and beliefs. It speaks the Model Context Protocol
over stdio, so Claude Code, Codex, Cursor and other MCP clients can call it.

Every answer comes from the deterministic Understand engine. No model writes any of them. The
binary links no generation package and no model client, and an architecture test
(`internal/archtest`) keeps it that way. It reads the project and writes nothing, unless you start
it with `--allow-corrections`.

## Install

With Node.js, nothing to install: `npx -y gen-understand --root /path/to/your-repo` fetches
the binary for your platform from the matching GitHub release, checks its SHA-256 and caches it.
The npm package `gen-understand` is also listed in the MCP Registry as
`io.github.hpdkhoa/gen-understand`. Set `UNDERSTAND_MCP_BINARY` to a local binary to skip the
download on an offline machine.

With the full install, `./install.sh` builds `understand-mcp` next to the other binaries.
`./install.sh --understand` builds it next to `understand-server`.

With Go 1.25 or newer, and no install script:

```bash
go install github.com/hpdkhoa/gen-system/generator/cmd/understand-mcp@latest
```

From a clone, `make mcp` builds `./understand-mcp`.

## Register it with your agent

Each client starts one server per project. Point `--root` at the codebase.

**Claude Code**

```bash
claude mcp add gen-understand -- understand-mcp --root /path/to/your-repo
# or, with Node.js and no install:
claude mcp add gen-understand -- npx -y gen-understand --root /path/to/your-repo
```

**Codex** (`~/.codex/config.toml`)

```toml
[mcp_servers.gen-understand]
command = "understand-mcp"
args = ["--root", "/path/to/your-repo"]
```

**Cursor** (`.cursor/mcp.json` in the project)

```json
{
  "mcpServers": {
    "gen-understand": {
      "command": "understand-mcp",
      "args": ["--root", "."]
    }
  }
}
```

Any other MCP client takes the same command and arguments.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--root` | `.` | The project to understand |
| `--copybooks` | none | COBOL copybook search path, separated like `PATH` |
| `--stdlib` | `auto` | Standard library roots: `auto`, `none`, or a list such as `go,java=/opt/jdk/java.base` |
| `--allow-corrections` | off | Adds `correct_belief`, which writes `.gen/beliefs.json` in the project |
| `--no-watch` | off | Keeps the index as it was at start instead of re-indexing changed files |
| `--version` | | Prints the version |

The server loads the project once at start. Large projects take a moment; it logs progress to stderr.

## Tools

| Tool | Answers |
|---|---|
| `architecture` | Languages, top packages, entry point candidates, constructs the parsers could not model, call edge resolution, attached standard libraries |
| `search_symbols` | Ranked matches over symbol names, with an optional effect filter. Returns the symbol ids the other tools take |
| `call_graph` | A walk over resolved call edges from one symbol, callers or callees, up to 4 hops. Unresolved callees show as external nodes |
| `control_flow` | The basic blocks of one function, their edges and the source lines in each |
| `source` | The source lines of one symbol |
| `file_symbols` | Every symbol in one file |
| `effects` | Functions grouped by what they touch: db, fs, network, pure or unknown |
| `beliefs` | What gen-system holds about each routine, filtered by subject, kind or verification state |
| `correct_belief` | Only with `--allow-corrections`. Records a human correction. It wins on every rebuild, and goes stale with both values shown when the code changes under it |

A symbol id looks like `<file>#<kind>:<qualified name>`. Start with `architecture`, find names
with `search_symbols`, then pass ids to the rest.

The server cuts any tool result over 200 KB, and the text says how to ask for less.

## Protocol

JSON-RPC 2.0 over stdio, one message per line. The server answers `initialize`, `ping`,
`tools/list` and `tools/call`, and supports protocol versions 2025-11-25, 2025-06-18, 2025-03-26
and 2024-11-05. Each tool result carries its JSON as text, plus `structuredContent` for clients
that read it. Stdout carries only protocol messages; every log line goes to stderr.

The protocol code is in `generator/internal/mcp` and uses only the Go standard library.
