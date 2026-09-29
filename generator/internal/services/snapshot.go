package services

import (
	"fmt"

	"github.com/hpdkhoa/gen-system/generator/internal/api/sse"
	"github.com/hpdkhoa/gen-system/generator/internal/safety"
	"github.com/hpdkhoa/gen-system/generator/internal/store"
)

type SnapshotService struct {
	st     *store.SnapshotStore
	broker *sse.Broker
	root   string
}

func NewSnapshotService(st *store.SnapshotStore, broker *sse.Broker, root string) *SnapshotService {
	return &SnapshotService{st: st, broker: broker, root: root}
}

func (s *SnapshotService) List(projectID string) []*store.SnapshotRecord {
	return s.st.All(projectID)
}

func (s *SnapshotService) Create(name, desc, author, projectID string, auto bool) (*store.SnapshotRecord, error) {
	// v26.58 (D096): a failed project copy was replaced by an empty SnapPath
	// and a nil error, so the API returned a normal record that a later
	// Restore could not restore. Return the error and store no record. The
	// auto flag was also dropped; the store now records it.
	snapPath, err := safety.SnapshotProject(s.root)
	if err != nil {
		return nil, fmt.Errorf("snapshot of %s failed: %w", s.root, err)
	}
	// v26.58 (D098): the store now reports a failed save, for example when
	// snapshots.json did not load; pass it on instead of a record.
	rec, err := s.st.Create(name, desc, author, projectID, snapPath, 0, 0, 0, auto)
	if err != nil {
		return nil, err
	}
	s.broker.Publish("snapshot_created", rec)
	return rec, nil
}

func (s *SnapshotService) Update(id, name, desc string, pinned *bool) *store.SnapshotRecord {
	rec := s.st.Update(id, name, desc, pinned)
	if rec != nil {
		s.broker.Publish("snapshot_updated", rec)
	}
	return rec
}

func (s *SnapshotService) Delete(id string) bool {
	ok := s.st.Delete(id)
	if ok {
		s.broker.Publish("snapshot_deleted", map[string]string{"id": id})
	}
	return ok
}

func (s *SnapshotService) Restore(id string) *store.SnapshotRecord {
	rec := s.st.Restore(id)
	if rec != nil {
		if rec.SnapPath != "" {
			_ = safety.RestoreSnapshot(s.root, rec.SnapPath)
		}
		s.broker.Publish("snapshot_restored", rec)
	}
	return rec
}

func (s *SnapshotService) Branch(id string) *store.SnapshotRecord {
	rec := s.st.Branch(id)
	if rec != nil {
		s.broker.Publish("snapshot_branched", rec)
	}
	return rec
}

func (s *SnapshotService) Find(id string) *store.SnapshotRecord {
	return s.st.Find(id)
}
