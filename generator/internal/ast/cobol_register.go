// cobol_register.go — registration of the COBOL frontend.
//
// v19: COBOL is now feature-complete (EVALUATE, COPY, REDEFINES, OCCURS,
// FILE-CONTROL/SELECT/FD, COMP-3, expanded CICS verbs) and registers in
// the default build. The cobol_preview build tag is no longer required.
//
// To use copybook resolution, instantiate CobolFrontend with
// CopybookPath populated instead of relying on the registry singleton.

package ast

func init() { Register(&CobolFrontend{}) }
