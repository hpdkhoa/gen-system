package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SnapshotRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
	Author      string `json:"author"`
	CreatedAt   int64  `json:"created_at"`
	Entities    int    `json:"entities"`
	Fields      int    `json:"fields"`
	Lines       int    `json:"lines"`
	Hash        string `json:"hash"`
	SchemaVer   string `json:"schema_version"`
	Pinned      bool   `json:"pinned"`
	Branched    bool   `json:"branched"`
	Auto        bool   `json:"auto"`
	ProjectID   string `json:"project_id"`
	SnapPath    string `json:"snap_path"`
}

type SnapshotStore struct {
	mu      sync.RWMutex
	records []*SnapshotRecord
	path    string
}

func NewSnapshotStore(root string) *SnapshotStore {
	s := &SnapshotStore{path: filepath.Join(root, ".gen", "snapshots.json")}
	s.load()
	return s
}

func (s *SnapshotStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.records)
}

func (s *SnapshotStore) save() {
	_ = os.MkdirAll(filepath.Dir(s.path), 0755)
	data, _ := json.MarshalIndent(s.records, "", "  ")
	_ = os.WriteFile(s.path, data, 0644)
}

func (s *SnapshotStore) All(projectID string) []*SnapshotRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*SnapshotRecord
	for _, r := range s.records {
		if projectID == "" || r.ProjectID == projectID {
			c := *r
			out = append(out, &c)
		}
	}
	return out
}

func (s *SnapshotStore) Find(id string) *SnapshotRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.records {
		if r.ID == id {
			c := *r
			return &c
		}
	}
	return nil
}

func (s *SnapshotStore) Create(name, desc, author, projectID, snapPath string, entities, fields, lines int) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.Tag == "CURRENT" && r.ProjectID == projectID {
			r.Tag = "SNAPSHOT"
		}
	}
	now := time.Now()
	h := sha256.Sum256([]byte(fmt.Sprintf("%d%s", now.UnixNano(), name)))
	rec := &SnapshotRecord{
		ID: fmt.Sprintf("snap-%d", now.UnixMilli()), Name: name,
		Description: desc, Tag: "CURRENT", Author: author,
		CreatedAt: now.UnixMilli(), Entities: entities,
		Fields: fields, Lines: lines,
		Hash:      fmt.Sprintf("SHA256:%x", h[:4]),
		SchemaVer: "v0.1", ProjectID: projectID, SnapPath: snapPath,
	}
	s.records = append([]*SnapshotRecord{rec}, s.records...)
	s.save()
	return rec
}

func (s *SnapshotStore) Update(id, name, desc string, pinned *bool) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.ID != id {
			continue
		}
		if name != "" {
			r.Name = name
		}
		if desc != "" {
			r.Description = desc
		}
		if pinned != nil {
			r.Pinned = *pinned
		}
		s.save()
		c := *r
		return &c
	}
	return nil
}

func (s *SnapshotStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.records {
		if r.ID == id {
			s.records = append(s.records[:i], s.records[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

func (s *SnapshotStore) Restore(id string) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var target *SnapshotRecord
	for _, r := range s.records {
		if r.ID == id {
			target = r
		}
	}
	if target == nil {
		return nil
	}
	for _, r := range s.records {
		if r.Tag == "CURRENT" && r.ProjectID == target.ProjectID {
			r.Tag = "SNAPSHOT"
		}
	}
	target.Tag = "CURRENT"
	s.save()
	c := *target
	return &c
}

func (s *SnapshotStore) Branch(id string) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var src *SnapshotRecord
	for _, r := range s.records {
		if r.ID == id {
			src = r
			break
		}
	}
	if src == nil {
		return nil
	}
	for _, r := range s.records {
		if r.Tag == "CURRENT" && r.ProjectID == src.ProjectID {
			r.Tag = "SNAPSHOT"
		}
	}
	now := time.Now()
	h := sha256.Sum256([]byte(fmt.Sprintf("%d", now.UnixNano())))
	branch := &SnapshotRecord{
		ID:          fmt.Sprintf("snap-%d", now.UnixMilli()),
		Name:        src.Name + " · branch",
		Description: fmt.Sprintf("Branched from \"%s\"", src.Name),
		Tag:         "CURRENT", Author: "you",
		CreatedAt: now.UnixMilli(), Entities: src.Entities,
		Fields: src.Fields, Lines: src.Lines,
		Hash:      fmt.Sprintf("SHA256:%x", h[:4]),
		SchemaVer: src.SchemaVer, ProjectID: src.ProjectID,
		SnapPath: src.SnapPath, Branched: true,
	}
	s.records = append([]*SnapshotRecord{branch}, s.records...)
	s.save()
	return branch
}
