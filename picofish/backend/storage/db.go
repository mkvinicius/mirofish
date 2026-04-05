// Package storage provides a lightweight JSON-file-based store.
// No CGO, no external database — pure Go, perfect for Raspberry Pi.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is a generic key-value map stored in a collection.
type Record map[string]interface{}

// Store is a simple, thread-safe file-backed JSON store.
type Store struct {
	mu          sync.RWMutex
	dir         string
	collections map[string]map[string]Record // collection -> id -> record
}

var DB *Store

// Init initializes the store at the given directory path.
func Init(path string) error {
	dir := path
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("storage init: %w", err)
	}
	DB = &Store{
		dir:         dir,
		collections: make(map[string]map[string]Record),
	}
	// Load existing data
	collections := []string{"projects", "graph_nodes", "graph_edges", "agents", "simulation_actions", "reports", "agent_memories", "sim_history"}
	for _, c := range collections {
		if err := DB.load(c); err != nil {
			return fmt.Errorf("load %s: %w", c, err)
		}
	}
	return nil
}

func (s *Store) load(collection string) error {
	s.collections[collection] = make(map[string]Record)
	path := filepath.Join(s.dir, collection+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // new collection
	}
	if err != nil {
		return err
	}
	var records map[string]Record
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	s.collections[collection] = records
	return nil
}

func (s *Store) save(collection string) error {
	data, err := json.MarshalIndent(s.collections[collection], "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, collection+".json")
	return os.WriteFile(path, data, 0644)
}

// Insert adds or replaces a record by ID.
func (s *Store) Insert(collection, id string, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collections[collection] == nil {
		s.collections[collection] = make(map[string]Record)
	}
	if _, exists := record["created_at"]; !exists {
		record["created_at"] = time.Now().Format(time.RFC3339)
	}
	record["id"] = id
	s.collections[collection][id] = record
	return s.save(collection)
}

// Update modifies specific fields of a record.
func (s *Store) Update(collection, id string, fields Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.collections[collection][id]
	if !ok {
		return fmt.Errorf("record %s not found in %s", id, collection)
	}
	for k, v := range fields {
		rec[k] = v
	}
	s.collections[collection][id] = rec
	return s.save(collection)
}

// Delete removes a record.
func (s *Store) Delete(collection, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collections[collection], id)
	return s.save(collection)
}

// Get retrieves a record by ID.
func (s *Store) Get(collection, id string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.collections[collection][id]
	return r, ok
}

// QueryFunc returns records where filter returns true, sorted by created_at desc.
func (s *Store) QueryFunc(collection string, filter func(Record) bool) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []Record
	for _, r := range s.collections[collection] {
		if filter == nil || filter(r) {
			results = append(results, r)
		}
	}
	// Sort by created_at descending
	sortByCreatedAt(results)
	return results
}

// QueryAll returns all records in a collection, sorted by created_at desc.
func (s *Store) QueryAll(collection string) []Record {
	return s.QueryFunc(collection, nil)
}

// DeleteWhere deletes all records where filter returns true.
func (s *Store) DeleteWhere(collection string, filter func(Record) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.collections[collection] {
		if filter(r) {
			delete(s.collections[collection], id)
		}
	}
	return s.save(collection)
}

// GetStr is a helper to get a string field from a record.
func GetStr(r Record, key string) string {
	if v, ok := r[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetInt is a helper to get an int field from a record.
func GetInt(r Record, key string) int {
	if v, ok := r[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case int64:
			return int(n)
		}
	}
	return 0
}

// sortByCreatedAt sorts records by created_at descending (newest first).
func sortByCreatedAt(records []Record) {
	// Simple insertion sort — collection sizes are small
	for i := 1; i < len(records); i++ {
		for j := i; j > 0; j-- {
			a := GetStr(records[j-1], "created_at")
			b := GetStr(records[j], "created_at")
			if a < b {
				records[j-1], records[j] = records[j], records[j-1]
			} else {
				break
			}
		}
	}
}
