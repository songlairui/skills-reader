package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/egoist/mygo"
)

// SnapshotInfo describes the local index snapshot of a skills directory: the
// git tree hash of every skill folder at the time it was taken. It is this
// app's own index, kept in its data directory, and catches changes to every
// skill, including the ones no lock file tracks.
type SnapshotInfo struct {
	TakenAt string `json:"takenAt"`
	Count   int    `json:"count"`
	Changed int    `json:"changed"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Path    string `json:"path"`
}

// snapshotsEnabled is turned off by tests so they leave no files behind.
var snapshotsEnabled = true

type snapshotFile struct {
	TakenAt string            `json:"takenAt"`
	Root    string            `json:"root"`
	Hashes  map[string]string `json:"hashes"`
}

func dataDir() string {
	if d, err := mygo.App.Path(mygo.PathUserData); err == nil && d != "" {
		return d
	}
	d := filepath.Join(homeDir(), ".skills-reader")
	os.MkdirAll(d, 0o755)
	return d
}

func rootKey(root string) string {
	sum := sha1.Sum([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:])[:10]
}

func snapshotPath(root string) string {
	return filepath.Join(dataDir(), "index-"+rootKey(root)+".json")
}

func readSnapshot(root string) *snapshotFile {
	data, err := os.ReadFile(snapshotPath(root))
	if err != nil {
		return nil
	}
	var s snapshotFile
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	return &s
}

func writeSnapshot(lib *Library) error {
	s := snapshotFile{TakenAt: time.Now().Format(time.RFC3339), Root: lib.Root, Hashes: map[string]string{}}
	for _, sk := range lib.Skills {
		if sk.Kind != KindMissing && sk.LocalHash != "" {
			s.Hashes[sk.ID] = sk.LocalHash
		}
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(snapshotPath(lib.Root), data, 0o644)
}

// applySnapshot marks each skill against the stored snapshot, taking the
// first snapshot when there is none yet.
func applySnapshot(lib *Library) {
	if !lib.Exists || !snapshotsEnabled {
		return
	}
	snap := readSnapshot(lib.Root)
	if snap == nil {
		if writeSnapshot(lib) != nil {
			return
		}
		snap = readSnapshot(lib.Root)
		if snap == nil {
			return
		}
	}
	info := &SnapshotInfo{TakenAt: snap.TakenAt, Count: len(snap.Hashes), Path: snapshotPath(lib.Root)}
	present := map[string]bool{}
	for i := range lib.Skills {
		sk := &lib.Skills[i]
		if sk.Kind == KindMissing || sk.LocalHash == "" {
			continue
		}
		present[sk.ID] = true
		old, ok := snap.Hashes[sk.ID]
		switch {
		case !ok:
			sk.Snapshot = "new"
			info.Added++
		case old != sk.LocalHash:
			sk.Snapshot = "changed"
			info.Changed++
		default:
			sk.Snapshot = "same"
		}
	}
	for id := range snap.Hashes {
		if !present[id] {
			lib.RemovedSinceSnapshot = append(lib.RemovedSinceSnapshot, id)
		}
	}
	sort.Strings(lib.RemovedSinceSnapshot)
	info.Removed = len(lib.RemovedSinceSnapshot)
	lib.Snapshot = info
}
