package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
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
	loadErr error // v26.58 (D098): set when snapshots.json exists but cannot be read
}

func NewSnapshotStore(root string) *SnapshotStore {
	s := &SnapshotStore{path: filepath.Join(root, ".gen", "snapshots.json")}
	if err := s.load(); err != nil {
		s.loadErr = err
		log.Printf("snapshots: %v; snapshot changes are refused until the file is fixed", err)
	}
	return s
}

// LoadErr reports why snapshots.json could not be loaded, or nil.
func (s *SnapshotStore) LoadErr() error {
	return s.loadErr
}

// v26.58 (D098): load and save dropped every error. A broken snapshots.json
// gave an empty store with no message and the next save overwrote the old
// records; a failed write went unnoticed. Now load reports the error, save
// refuses to write over a file that did not load, and save writes a temp
// file and renames it so an interrupted write cannot truncate the file.
func (s *SnapshotStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.path, err)
	}
	if err := json.Unmarshal(data, &s.records); err != nil {
		s.records = nil
		return fmt.Errorf("parsing %s: %w", s.path, err)
	}
	return nil
}

func (s *SnapshotStore) save() error {
	if s.loadErr != nil {
		return fmt.Errorf("not saving snapshots: %w", s.loadErr)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// cloneLocked copies the records so a change can be undone when save fails.
func (s *SnapshotStore) cloneLocked() []*SnapshotRecord {
	out := make([]*SnapshotRecord, 0, len(s.records))
	for _, r := range s.records {
		c := *r
		out = append(out, &c)
	}
	return out
}

// saveOrUndoLocked saves, and on failure puts prev back and logs the error,
// so memory never shows a change the file does not hold.
func (s *SnapshotStore) saveOrUndoLocked(prev []*SnapshotRecord) error {
	if err := s.save(); err != nil {
		s.records = prev
		log.Printf("snapshots: %v", err)
		return err
	}
	return nil
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

// Create stores a new current record. v26.58 (D096): auto is stored in the
// record; before, the parameter did not exist and every record had auto=false.
func (s *SnapshotStore) Create(name, desc, author, projectID, snapPath string, entities, fields, lines int, auto bool) (*SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cloneLocked()
	for _, r := range s.records {
		if r.Tag == "CURRENT" && r.ProjectID == projectID {
			r.Tag = "SNAPSHOT"
		}
	}
	now := time.Now()
	h := sha256.Sum256([]byte(fmt.Sprintf("%d%s", now.UnixNano(), name)))
	rec := &SnapshotRecord{
		ID: s.newIDLocked(now), Name: name,
		Description: desc, Tag: "CURRENT", Author: author,
		CreatedAt: now.UnixMilli(), Entities: entities,
		Fields: fields, Lines: lines,
		Hash:      fmt.Sprintf("SHA256:%x", h[:4]),
		SchemaVer: "v0.1", ProjectID: projectID, SnapPath: snapPath,
		Auto: auto,
	}
	s.records = append([]*SnapshotRecord{rec}, s.records...)
	if err := s.saveOrUndoLocked(prev); err != nil {
		return nil, err
	}
	return rec, nil
}

// newIDLocked returns snap-<unix ms>, with a -2, -3, ... suffix when a record
// already has that ID. The caller holds s.mu.
//
// v26.58 (D097): the ID was the millisecond clock alone, so two Create or
// Branch calls in one millisecond gave two records one ID; Find and Delete
// then hit the newer record and Restore the older one.
func (s *SnapshotStore) newIDLocked(now time.Time) string {
	base := fmt.Sprintf("snap-%d", now.UnixMilli())
	id := base
	for i := 2; s.hasIDLocked(id); i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

func (s *SnapshotStore) hasIDLocked(id string) bool {
	for _, r := range s.records {
		if r.ID == id {
			return true
		}
	}
	return false
}

func (s *SnapshotStore) Update(id, name, desc string, pinned *bool) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cloneLocked()
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
		if s.saveOrUndoLocked(prev) != nil {
			return nil
		}
		c := *r
		return &c
	}
	return nil
}

func (s *SnapshotStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cloneLocked()
	for i, r := range s.records {
		if r.ID == id {
			s.records = append(s.records[:i], s.records[i+1:]...)
			if s.saveOrUndoLocked(prev) != nil {
				return false
			}
			return true
		}
	}
	return false
}

func (s *SnapshotStore) Restore(id string) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cloneLocked()
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
	if s.saveOrUndoLocked(prev) != nil {
		return nil
	}
	c := *target
	return &c
}

func (s *SnapshotStore) Branch(id string) *SnapshotRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cloneLocked()
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
		ID:          s.newIDLocked(now),
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
	if s.saveOrUndoLocked(prev) != nil {
		return nil
	}
	return branch
}
