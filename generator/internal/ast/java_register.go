// java_register.go (v23.12, Phase 4): registers the Java frontend in the
// default build so context.LoadContext discovers it via the registry. Mirrors
// cobol_register.go. Java is pure-Go and parse-only (no AST rewriter yet), like
// COBOL and CA Gen.
package ast

func init() { Register(&JavaFrontend{}) }
