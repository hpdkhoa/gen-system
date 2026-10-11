// index_removefile.go: intentionally empty after Phase A consolidation.
//
// Before Phase A: this file held a standalone RemoveFile method and a
// fileFromSymbolID helper. Both were absorbed into index.go so that the
// symbols package owns a single, coherent set of declarations.
//
// This file is left as a placeholder so the zip-overlay install does not
// leave a stale file with duplicate declarations on your disk. It is safe
// to delete from the repository at any time. The build does not depend
// on it.
//
// To remove cleanly:
//
//	git rm generator/internal/symbols/index_removefile.go
//
// All RemoveFile tests previously in index_removefile_test.go continue to
// work against the implementation now living in index.go.
package symbols
