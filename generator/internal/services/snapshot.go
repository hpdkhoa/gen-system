package services

import (
	"gen-system/generator/internal/api/sse"
	"gen-system/generator/internal/safety"
	"gen-system/generator/internal/store"
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
	snapPath, err := safety.SnapshotProject(s.root)
	if err != nil {
		snapPath = ""
	}
	rec := s.st.Create(name, desc, author, projectID, snapPath, 0, 0, 0)
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
