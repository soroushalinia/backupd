package state

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xero/backupd/internal/config"
	"go.etcd.io/bbolt"
)

var bucketName = []byte("snapshots")

type Store struct {
	db *bbolt.DB
}

func New(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("opening state db: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) RecordSnapshot(snap config.Snapshot) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		data, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%s/%s", snap.Plan, snap.ID)
		return b.Put([]byte(key), data)
	})
}

func (s *Store) ListSnapshots(plan string) ([]config.Snapshot, error) {
	var snaps []config.Snapshot
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		c := b.Cursor()
		prefix := []byte(plan + "/")
		for k, v := c.Seek(prefix); k != nil && len(k) >= len(prefix) && string(k[:len(prefix)]) == plan+"/"; k, v = c.Next() {
			var snap config.Snapshot
			if err := json.Unmarshal(v, &snap); err != nil {
				return err
			}
			snaps = append(snaps, snap)
		}
		return nil
	})
	return snaps, err
}

func (s *Store) LastSnapshot(plan string) (*config.Snapshot, error) {
	snaps, err := s.ListSnapshots(plan)
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, nil
	}
	var last *config.Snapshot
	for i := range snaps {
		if last == nil || snaps[i].Timestamp.After(last.Timestamp) {
			last = &snaps[i]
		}
	}
	return last, nil
}

func (s *Store) DeleteSnapshot(plan, snapID string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		key := fmt.Sprintf("%s/%s", plan, snapID)
		return b.Delete([]byte(key))
	})
}

func (s *Store) Close() error {
	return s.db.Close()
}

type RunRecord struct {
	ID        string    `json:"id"`
	Plan      string    `json:"plan"`
	StartedAt time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Size      int64     `json:"size"`
}
