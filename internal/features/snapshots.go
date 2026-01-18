package features

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Supported Time Formats
var TimeLayouts = []string{
	"02-01-2006-15-04-MST", // The standard format used by this app
	"2006-01-02-15-04-MST", 
	time.RFC3339,
}

// Exported Helper: Try multiple formats or fallback to file mod time
func ParseSnapshotTime(e os.DirEntry) time.Time {
	name := e.Name()
	for _, layout := range TimeLayouts {
		if t, err := time.Parse(layout, name); err == nil {
			return t
		}
	}
	// Fallback to File Info (Modification Time)
	info, _ := e.Info()
	return info.ModTime()
}

// --- Snapshot Listing ---
func HandleListSnapshots(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	var dest string
	core.State.Mu.Lock()
	for _, j := range core.State.Config.Jobs {
		if j.ID == jobID { dest = j.Dest; break }
	}
	core.State.Mu.Unlock()

	if dest == "" { http.Error(w, "Job not found", 404); return }

	entries, err := os.ReadDir(dest)
	if err != nil { http.Error(w, err.Error(), 500); return }

	type SnapshotItem struct {
		Name    string    `json:"name"`
		Date    string    `json:"date"`
		SortKey time.Time `json:"-"`
		Path    string    `json:"path"`
	}
	var list []SnapshotItem
	
	for _, e := range entries {
		if e.IsDir() {
			t := ParseSnapshotTime(e)
			list = append(list, SnapshotItem{
				Name:    e.Name(),
				Date:    t.Format("Jan 02, 2006 15:04 MST"),
				SortKey: t,
				Path:    filepath.Join(dest, e.Name()),
			})
		}
	}
	
	sort.Slice(list, func(i, j int) bool { return list[i].SortKey.After(list[j].SortKey) })
	json.NewEncoder(w).Encode(list)
}

// --- Job Stats ---
func HandleJobStats(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	var dest string
	core.State.Mu.Lock()
	for _, j := range core.State.Config.Jobs {
		if j.ID == jobID { dest = j.Dest; break }
	}
	core.State.Mu.Unlock()

	if dest == "" { http.Error(w, "Job not found", 404); return }

	entries, _ := os.ReadDir(dest)
	var oldest time.Time
	var count int
	first := true

	for _, e := range entries {
		if e.IsDir() {
			t := ParseSnapshotTime(e)
			if first || t.Before(oldest) {
				oldest = t
				first = false
			}
			count++
		}
	}

	oldestStr := "None"
	if count > 0 {
		oldestStr = oldest.Format("Jan 02, 2006")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": count,
		"oldest": oldestStr,
	})
}

// --- Diff & Rollback ---
func HandleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
	snapA := r.URL.Query().Get("a")
	snapB := r.URL.Query().Get("b")
	if snapA == "" || snapB == "" { http.Error(w, "Need two snapshots", 400); return }

	core.State.Mu.Lock()
	dest := ""
	for _, j := range core.State.Config.Jobs {
		if strings.HasPrefix(snapA, j.Dest) { dest = j.Dest; break }
	}
	core.State.Mu.Unlock()
	
	if dest == "" { http.Error(w, "Unknown snapshot path", 403); return }

	cmdStr := fmt.Sprintf("btrfs send --no-data -p '%s' '%s' | btrfs receive --dump", snapA, snapB)
	id := core.RunCommandAsync("DIFF", "🔍", "Compare", "sh", "-c", cmdStr)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

func HandleRollback(w http.ResponseWriter, r *http.Request) {
	snapPath := r.URL.Query().Get("path")
	jobID := r.URL.Query().Get("job_id")
	
	var job config.BackupJob
	found := false
	core.State.Mu.Lock()
	for _, j := range core.State.Config.Jobs {
		if j.ID == jobID { job = j; found = true; break }
	}
	core.State.Mu.Unlock()

	if !found { http.Error(w, "Job not found", 404); return }
	if !strings.HasPrefix(snapPath, job.Dest) { http.Error(w, "Invalid snap path", 403); return }

	go func() {
		backupName := fmt.Sprintf("%s_BEFORE_ROLLBACK_%d", job.Source, time.Now().Unix())
		core.RunCommandAsync("ROLLBACK", "📦", job.Source, "mv", job.Source, backupName)
		time.Sleep(1 * time.Second)
		core.RunCommandAsync("ROLLBACK", "♻️", job.Source, "btrfs", "subvolume", "snapshot", snapPath, job.Source)
	}()

	json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})
}

func HandleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" { http.Error(w, "Path required", 400); return }
	
	allowed := false
	core.State.Mu.Lock()
	for _, j := range core.State.Config.Jobs {
		if strings.HasPrefix(filepath.Clean(path), filepath.Clean(j.Dest)) {
			allowed = true; break
		}
	}
	core.State.Mu.Unlock()
	
	if !allowed { http.Error(w, "Forbidden", 403); return }

	core.RunCommandAsync("DELETE SNAP", "🗑", path, "btrfs", "subvolume", "delete", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}

// --- Core Snapshot Logic ---
func PerformBackupJob(job config.BackupJob) {
	os.MkdirAll(job.Dest, 0755)
	now := time.Now()
	name := now.Format(TimeLayouts[0])
	fullDest := filepath.Join(job.Dest, name)
	
	core.PrintConsole(config.LogLevelDefault, "Starting Job: %s", job.Name)

	if job.PreCommand != "" {
		core.RunCommandAsync("HOOK-PRE", "🪝", job.Name, "sh", "-c", job.PreCommand)
		time.Sleep(1 * time.Second)
	}

	cmd := exec.Command("btrfs", "subvolume", "snapshot", "-r", job.Source, fullDest)
	out, err := cmd.CombinedOutput()
	status := "Success"
	if err != nil { status = "Failed" }
	
	core.State.Mu.Lock()
	core.State.History = append([]core.LogEntry{{
		ID: time.Now().UnixNano(), Type: "SNAPSHOT", Emoji: "📸", 
		Path: fullDest, Timestamp: now.Format(time.RFC3339), 
		Status: status, Output: string(out), Duration: "0s",
	}}, core.State.History...)
	core.State.Mu.Unlock()
	core.SaveState()

	if status == "Success" {
		if job.Replication.Enabled && job.Replication.TargetDest != "" {
			replCmd := fmt.Sprintf("btrfs send '%s' | btrfs receive '%s'", fullDest, job.Replication.TargetDest)
			core.RunCommandAsync("REPLICATE", "🚀", job.Replication.TargetDest, "sh", "-c", replCmd)
		}
		if job.PostCommand != "" {
			core.RunCommandAsync("HOOK-POST", "🪝", job.Name, "sh", "-c", job.PostCommand)
		}
		EnforceRetention(job.Dest, job.Retention)
	}
}

func EnforceRetention(destPath string, cfg config.RetentionConfig) {
	if !cfg.Enabled { return }
	entries, _ := os.ReadDir(destPath)
	
	type Snap struct { Name string; Time time.Time }
	var snaps []Snap
	for _, e := range entries {
		t := ParseSnapshotTime(e)
		snaps = append(snaps, Snap{Name: e.Name(), Time: t})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })

	var toDel []string
	if cfg.Mode == "count" && len(snaps) > cfg.Value {
		for _, s := range snaps[cfg.Value:] { toDel = append(toDel, s.Name) }
	} else if cfg.Mode == "time" {
		var cutoff time.Time
		now := time.Now()
		switch cfg.Unit {
		case "days": cutoff = now.AddDate(0, 0, -cfg.Value)
		case "weeks": cutoff = now.AddDate(0, 0, -cfg.Value*7)
		case "months": cutoff = now.AddDate(0, -cfg.Value, 0)
		case "years": cutoff = now.AddDate(-cfg.Value, 0, 0)
		}
		for _, s := range snaps {
			if s.Time.Before(cutoff) { toDel = append(toDel, s.Name) }
		}
	}
	
	for _, name := range toDel {
		p := filepath.Join(destPath, name)
		exec.Command("btrfs", "subvolume", "delete", p).Run()
	}
	if len(toDel) > 0 {
		core.PrintConsole(config.LogLevelDefault, "Retention deleted %d snapshots in %s", len(toDel), destPath)
	}
}
