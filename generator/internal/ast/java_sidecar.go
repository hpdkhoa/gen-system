// java_sidecar.go — v23.32 (stdlib provenance / Java sidecar seam): PLACEHOLDER.
//
// This defines — but does not yet implement — the seam a future JVM-side Java
// analyzer will satisfy. Rationale (decided with the project owner): the pure-Go
// JavaFrontend stays the default and the thing we demo; it gives signature-level
// understanding and resolves into a JDK signature tree (see the second-language
// proof in context/stdlib_java_test.go). Full-fidelity comprehension of COMPILED
// Java — bytecode-only JARs, real method bodies/CFGs inside the JDK, lambdas and
// generics the hand-rolled parser pushes to NodeUnknown — is out of scope for the
// pure-Go reader and belongs to a JVM sidecar.
//
// CONTRACT the sidecar must honour when it lands:
//   - It is a Frontend like any other: it produces a *Module in the SAME unified
//     IR, so the symbol index, CFG builder, resolver, provenance tier, skill
//     export and Understand UI all work on its output with zero new code.
//   - It runs an external process (a bundled JAR over a vendored JRE). Therefore
//     Available() probes for that process and returns false when absent — and the
//     registry/presentation layers already treat a registered-but-unavailable
//     frontend as a first-class state.
//   - It is NEVER on the default install path and is NOT registered by init().
//     A caller opts in explicitly (e.g. a future `--java-sidecar <endpoint>`
//     flag wiring RegisterJavaSidecar). This keeps "offline single static
//     binary, no surprises" literally true for the default build.
//
// Because it is unregistered and Available()==false, this type is inert today:
// it compiles, documents the boundary, and lets later sidecar work be a drop-in
// rather than a refactor. It deliberately does NOT claim LangJava (the pure-Go
// JavaFrontend owns that language and the .java extension); registering a second
// frontend for LangJava would panic. The sidecar, when real, will either replace
// the pure-Go registration behind the opt-in flag or claim a distinct language
// id such as "java-bytecode" for .class/.jar inputs.
package ast

import "errors"

// LangJavaSidecar is a DISTINCT language id reserved for the JVM sidecar's
// compiled-Java inputs (.class/.jar). It is intentionally not LangJava so the
// placeholder can be registered for experimentation without colliding with the
// pure-Go source frontend. Not used until the sidecar is implemented.
const LangJavaSidecar Language = "java-bytecode"

// JavaSidecarFrontend is the placeholder for the future JVM-side analyzer.
// All methods are defined so it satisfies Frontend; Parse/Emit return a clear
// not-implemented error and Available() reports false so nothing invokes it.
type JavaSidecarFrontend struct {
	// Endpoint is where the sidecar process would be reached (e.g. a unix
	// socket or localhost port). Empty until wired. Stored now so the struct
	// shape is stable for the implementing change.
	Endpoint string
}

// ErrJavaSidecarNotImplemented is returned by the placeholder's Parse/Emit.
var ErrJavaSidecarNotImplemented = errors.New(
	"java sidecar frontend is a placeholder; the JVM-side analyzer is not implemented yet")

func (j *JavaSidecarFrontend) Language() Language   { return LangJavaSidecar }
func (j *JavaSidecarFrontend) Extensions() []string { return []string{".class", ".jar"} }

// Available reports false until a sidecar process is actually reachable. The
// future implementation will probe j.Endpoint here.
func (j *JavaSidecarFrontend) Available() bool { return false }

// Parse will, in the real implementation, ship source/bytecode to the sidecar
// and unmarshal the returned unified-IR JSON into a *Module. Today it is inert.
func (j *JavaSidecarFrontend) Parse(filePath string, source []byte) (*Module, error) {
	return nil, ErrJavaSidecarNotImplemented
}

// Emit is not supported: the sidecar is an analysis (read-only) frontend.
func (j *JavaSidecarFrontend) Emit(module *Module) ([]byte, error) {
	return nil, ErrJavaSidecarNotImplemented
}

// RegisterJavaSidecar wires the placeholder into the registry. It is NOT called
// from any init() — registration is an explicit, opt-in act reserved for the
// change that implements the sidecar. Provided now so that change is a one-line
// call rather than new plumbing.
func RegisterJavaSidecar(endpoint string) {
	Register(&JavaSidecarFrontend{Endpoint: endpoint})
}
