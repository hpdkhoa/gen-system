# gen-understand

gen-system's Understand engine as an MCP server. It gives an AI agent parser facts about a
codebase: the architecture, symbols, call graphs, control flow graphs, effects, source and the
beliefs gen-system holds about each routine. No model writes these answers; deterministic parsers
for Go, COBOL with copybooks, CA Gen, Java, TypeScript and Python do.

The server is read only. It runs locally and sends nothing anywhere.

## Use it

**Claude Code**

```bash
claude mcp add gen-understand -- npx -y gen-understand --root /path/to/your-repo
```

**Codex** (`~/.codex/config.toml`)

```toml
[mcp_servers.gen-understand]
command = "npx"
args = ["-y", "gen-understand", "--root", "/path/to/your-repo"]
```

**Cursor** (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "gen-understand": {
      "command": "npx",
      "args": ["-y", "gen-understand", "--root", "."]
    }
  }
}
```

## Tools

`architecture`, `search_symbols`, `call_graph`, `control_flow`, `source`, `file_symbols`, `effects`
and `beliefs`. With `--allow-corrections`, also `correct_belief`. The full list, the flags and the
protocol details are in
[docs/mcp.md](https://github.com/hpdkhoa/gen-system/blob/main/docs/mcp.md).

## How this package works

The server is a Go binary. On first run this package downloads the binary for your platform from
the [gen-system release](https://github.com/hpdkhoa/gen-system/releases) of the same version,
checks its SHA-256 against the release's `checksums.txt`, and caches it. Later runs start at once
and work offline. Builds exist for Linux, macOS and Windows, on x64 and arm64.

| Variable | Use |
|---|---|
| `UNDERSTAND_MCP_BINARY` | Path of a local `understand-mcp` binary. Skips the download, for offline machines |
| `UNDERSTAND_MCP_CACHE` | Folder for the downloaded binary. Default: the user cache folder |

Without npm, install the binary with Go: `go install github.com/hpdkhoa/gen-system/generator/cmd/understand-mcp@latest`.

## License

Apache-2.0.
