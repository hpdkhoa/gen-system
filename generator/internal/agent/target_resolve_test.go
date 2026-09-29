// generator/internal/agent/target_resolve_test.go — v23.15: locks in the heal
// target-file resolution against the generator's REAL filename conventions, so a
// future change can't silently reintroduce the casing/dir mismatch that made
// every handler/validation self-heal fail with "no such file or directory".
package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEmpty(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTargetFileFor_MatchesGeneratorConventions(t *testing.T) {
	root := t.TempDir()
	// Mirror the exact files the generator emits for entity "Customer".
	for _, rel := range []string{
		"backend/internal/model/customer.go",
		"backend/internal/store/customer.go",
		"backend/internal/service/customer_service.go",
		"backend/internal/service/customer_service_test.go",
		"backend/internal/handler/Customer_handler.go",
		"backend/internal/handler/Customer_handler_test.go",
		"backend/internal/handler/Customer_service_handler.go",
		"backend/internal/validation/Customer_validation.go",
	} {
		writeEmpty(t, filepath.Join(root, rel))
	}

	cases := map[string]string{
		"generate_model":      "backend/internal/model/customer.go",
		"generate_repo":       "backend/internal/store/customer.go",
		"generate_service":    "backend/internal/service/customer_service.go",
		"generate_handler":    "backend/internal/handler/Customer_handler.go",
		"generate_validation": "backend/internal/validation/Customer_validation.go",
	}
	for typ, want := range cases {
		got := targetFileFor("Customer", typ, root)
		if got != filepath.Join(root, want) {
			t.Errorf("%s: got %q, want %q", typ, got, filepath.Join(root, want))
		}
	}

	// A handler task must resolve to the plain handler, never the _test.go or
	// the _service_handler.go sibling.
	if got := targetFileFor("Customer", "generate_handler", root); filepath.Base(got) != "Customer_handler.go" {
		t.Errorf("handler resolved to wrong sibling: %q", got)
	}

	// Case-insensitive fallback: an entity name in a different case still finds
	// the real file on disk.
	//
	// v26.25: this assertion only means anything on a case-SENSITIVE filesystem.
	// On Windows and on macOS's default volume, os.Stat("CUSTOMER_handler.go")
	// succeeds against the file created as "Customer_handler.go", so
	// targetFileFor returns the requested spelling without ever reaching the
	// fallback scan. That is correct behaviour and still a valid path to the
	// same file, so the old unconditional check reported a failure that did not
	// exist. Detect the filesystem and assert accordingly.
	if _, err := os.Stat(filepath.Join(root, "backend/internal/handler/CUSTOMER_handler.go")); err == nil {
		t.Log("case-insensitive filesystem: OS resolves the name, fallback scan not exercised")
	} else if got := targetFileFor("CUSTOMER", "generate_handler", root); filepath.Base(got) != "Customer_handler.go" {
		t.Errorf("case-insensitive fallback failed: %q", got)
	}

	// Unknown task type yields no file.
	if got := targetFileFor("Customer", "generate_env", root); got != "" {
		t.Errorf("unknown task type should yield empty, got %q", got)
	}
}

func TestPruneOrphanEntityFiles(t *testing.T) {
	root := t.TempDir()
	// Simulate a prior run that generated User/Stock plus shared files.
	for _, rel := range []string{
		"backend/internal/model/user.go",
		"backend/internal/model/stock.go",
		"backend/internal/store/user.go",
		"backend/internal/store/stock.go",
		"backend/internal/store/interfaces.go", // shared — must survive
		"backend/internal/service/user_service.go",
		"backend/internal/service/user_service_test.go",
		"backend/internal/service/stock_service.go",
		"backend/internal/handler/User_handler.go",
		"backend/internal/handler/User_handler_test.go",
		"backend/internal/handler/User_service_handler.go",
		"backend/internal/handler/handler.go", // shared — must survive
		"backend/internal/validation/User_validation.go",
		"backend/internal/validation/require.go", // shared — must survive
		"backend/main.go",                        // shared — must survive
	} {
		writeEmpty(t, filepath.Join(root, rel))
	}

	// Current run only has Order.
	pruneOrphanEntityFiles(root, []string{"Order"})

	mustGone := []string{
		"backend/internal/model/user.go",
		"backend/internal/model/stock.go",
		"backend/internal/store/user.go",
		"backend/internal/store/stock.go",
		"backend/internal/service/user_service.go",
		"backend/internal/service/user_service_test.go",
		"backend/internal/service/stock_service.go",
		"backend/internal/handler/User_handler.go",
		"backend/internal/handler/User_handler_test.go",
		"backend/internal/handler/User_service_handler.go",
		"backend/internal/validation/User_validation.go",
	}
	for _, rel := range mustGone {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("orphan should have been pruned: %s", rel)
		}
	}

	mustSurvive := []string{
		"backend/internal/store/interfaces.go",
		"backend/internal/handler/handler.go",
		"backend/internal/validation/require.go",
		"backend/main.go",
	}
	for _, rel := range mustSurvive {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("shared file must survive prune: %s (%v)", rel, err)
		}
	}
}

func TestPruneKeepsCurrentEntities(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"backend/internal/service/order_service.go",
		"backend/internal/service/product_service.go",
		"backend/internal/handler/Order_handler.go",
	} {
		writeEmpty(t, filepath.Join(root, rel))
	}
	pruneOrphanEntityFiles(root, []string{"Order", "Product"})
	for _, rel := range []string{
		"backend/internal/service/order_service.go",
		"backend/internal/service/product_service.go",
		"backend/internal/handler/Order_handler.go",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("current-entity file wrongly pruned: %s", rel)
		}
	}
}

// v26.57 (D001): Order_service_handler.go also ends in "_handler.go". Before the
// fix the "_handler.go" row read its stem as "order_service", which is never a
// valid entity, and deleted the business-route file of a current entity.
func TestPruneKeepsServiceHandlerOfCurrentEntity(t *testing.T) {
	root := t.TempDir()
	keep := []string{
		"backend/internal/handler/Order_handler.go",
		"backend/internal/handler/Order_handler_test.go",
		"backend/internal/handler/Order_service_handler.go",
		"backend/internal/service/order_service.go",
		"backend/internal/service/order_service_test.go",
	}
	gone := []string{
		"backend/internal/handler/User_service_handler.go",
	}
	for _, rel := range append(append([]string{}, keep...), gone...) {
		writeEmpty(t, filepath.Join(root, rel))
	}
	pruneOrphanEntityFiles(root, []string{"Order"})
	for _, rel := range keep {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("current-entity file wrongly pruned: %s", rel)
		}
	}
	for _, rel := range gone {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("orphan should have been pruned: %s", rel)
		}
	}
}

func TestSanitizeIdent(t *testing.T) {
	cases := map[string]string{
		"Customer":   "Customer",
		"order":      "Order",
		"due date":   "DueDate",
		"order_item": "OrderItem",
		"123abc":     "F123abc",
		"":           "",
	}
	for in, want := range cases {
		if got := sanitizeIdent(in); got != want {
			t.Errorf("sanitizeIdent(%q) = %q, want %q", in, got, want)
		}
	}
}
