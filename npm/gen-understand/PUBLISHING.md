# Publishing gen-understand

One release puts out three things, in this order: the binaries on GitHub, the npm package, and
the MCP Registry listing. The registry accepts the listing only after the npm package is live and
its `package.json` carries `"mcpName": "io.github.hpdkhoa/gen-understand"`.

## Once

1. Make an npm account and an automation token (npmjs.com, Access Tokens, Generate New Token,
   Automation).
2. In the GitHub repository, add it as the secret `NPM_TOKEN` (Settings, Secrets and variables,
   Actions).

The MCP Registry step signs in with GitHub OIDC from the workflow, so it needs no token.

## Each release

1. Set the same version in three places:
   - `npm/gen-understand/package.json`: `version`
   - `npm/gen-understand/server.json`: `version` and `packages[0].version`
2. Commit, then tag and push:

   ```bash
   git tag understand-mcp-v0.1.0
   git push origin understand-mcp-v0.1.0
   ```

`.github/workflows/understand-mcp-release.yml` then:

1. checks that the tag and both files name the same version, and stops if not;
2. runs the MCP tests and the architecture test;
3. builds six binaries (Linux, macOS, Windows; x64 and arm64), writes `checksums.txt`, and makes
   the GitHub release `understand-mcp-vX.Y.Z` with them;
4. tests the launcher and publishes the npm package with provenance;
5. publishes `server.json` to the MCP Registry.

The npm package fetches its binary from the release of its own version, so the release must exist
before anyone runs the package. The order of the jobs makes sure of that.

## By hand, without the workflow

```bash
# binaries: build the six files as the workflow does, then
gh release create understand-mcp-v0.1.0 dist/*

# npm
cd npm/gen-understand
npm test
npm publish --access public

# registry (mcp-publisher from https://github.com/modelcontextprotocol/registry/releases)
mcp-publisher login github
mcp-publisher publish
```

## Check it

- `npx -y gen-understand --version` prints the version.
- https://registry.modelcontextprotocol.io/v0/servers?search=gen-understand lists it.
