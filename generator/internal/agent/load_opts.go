// generator/internal/agent/load_opts.go — v26.34: how the agent loads a
// project through the Understand engine.
//
// Until v26.34 the agent and the verify stage called the plain loader, which
// never attaches a standard library, while understand-server and genbench
// had a --stdlib flag. So a generated Go backend was verified with no
// knowledge of net/http, encoding/json or testing, and every call into them
// was an unresolved edge. The default is now auto detection: the loader
// looks at the languages present in the project and attaches the matching
// runtime (GOROOT/src for Go, the built-in catalog for COBOL), import scoped,
// so only the packages the project actually imports are parsed. The gen CLI
// sets this from --stdlib or GEN_STDLIB; "none" opts out.
package agent

import agentctx "gen-system/generator/internal/context"

// LoadOpts is used by every LoadContext call the generation path makes.
var LoadOpts = agentctx.LoadContextOpts{StdlibAuto: true}
