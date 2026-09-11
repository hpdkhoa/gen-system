package safety

import (
	"fmt"
)

// SafeExecute wraps any mutation with snapshot + build verification.
//
// Flow:
//  1. SnapshotProject(root)   — copy full tree to .gen/snapshots/
//  2. fn()                    — run the mutation
//  3. ValidateBuild(root)     — go build ./...
//  4. On any failure: RestoreSnapshot() and return the error
//
// The build error output is now propagated to the caller so it can
// be used by the intelligence repair pipeline (ClassifyError, HashError).
func SafeExecute(root string, fn func() error) error {
	fmt.Println("📸 Creating snapshot...")
	snap, err := SnapshotProject(root)
	if err != nil {
		return err
	}

	fmt.Println("🧬 Running mutation...")
	if err = fn(); err != nil {
		fmt.Println("❌ Mutation failed, rolling back...")
		_ = RestoreSnapshot(root, snap)
		return err
	}

	fmt.Println("🔨 Validating build...")
	buildOutput, buildErr := ValidateBuild(root)
	if buildErr != nil {
		if buildOutput != "" {
			fmt.Println("❌ Build errors:\n" + buildOutput)
		}
		fmt.Println("❌ Build failed, rolling back...")
		_ = RestoreSnapshot(root, snap)
		return buildErr
	}

	fmt.Println("✅ Mutation success")
	return nil
}

// SafeExecuteWithOutput is like SafeExecute but returns the build output
// on failure so callers can pass it to the intelligence repair pipeline.
func SafeExecuteWithOutput(root string, fn func() error) (string, error) {
	fmt.Println("📸 Creating snapshot...")
	snap, err := SnapshotProject(root)
	if err != nil {
		return "", err
	}

	fmt.Println("🧬 Running mutation...")
	if err = fn(); err != nil {
		fmt.Println("❌ Mutation failed, rolling back...")
		_ = RestoreSnapshot(root, snap)
		return "", err
	}

	fmt.Println("🔨 Validating build...")
	buildOutput, buildErr := ValidateBuild(root)
	if buildErr != nil {
		if buildOutput != "" {
			fmt.Println("❌ Build errors:\n" + buildOutput)
		}
		fmt.Println("❌ Build failed, rolling back...")
		_ = RestoreSnapshot(root, snap)
		return buildOutput, buildErr
	}

	fmt.Println("✅ Mutation success")
	return "", nil
}
