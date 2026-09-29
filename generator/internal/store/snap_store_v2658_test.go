// D097: Create and Branch built the ID from the millisecond clock only, so two
// calls in the same millisecond shared one ID and Find, Delete and Restore
// acted on different records.
package store

import "testing"

func TestSnapStore_IDsUniqueWithinOneMillisecond(t *testing.T) {
	s := NewSnapshotStore(t.TempDir())
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		rec, _ := s.Create("n", "", "you", "default", "", 0, 0, 0, false)
		if seen[rec.ID] {
			t.Fatalf("Create gave the ID %s twice", rec.ID)
		}
		seen[rec.ID] = true
	}
	src, _ := s.Create("src", "", "you", "default", "", 0, 0, 0, false)
	br := s.Branch(src.ID)
	if br == nil || br.ID == src.ID {
		t.Fatalf("Branch reused the source ID %s", src.ID)
	}
	if !s.Delete(br.ID) || s.Find(br.ID) != nil {
		t.Fatalf("after Delete the branch ID %s still finds a record", br.ID)
	}
	if got := s.Find(src.ID); got == nil || got.Name != "src" {
		t.Fatalf("Find(%s) = %+v, want the source record", src.ID, got)
	}
}
