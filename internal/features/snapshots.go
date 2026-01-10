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

const TimeLayout = "02-01-2006-15-04-MST"

// --- Diff Feature ---
func HandleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
	snapA := r.URL.Query().Get("a")
	snapB := r.URL.Query().Get("b")
	if snapA == "" || snapB == "" { http.Error(w, "Need two snapshots", 400); return }

	// Security: simple check
	core.State.Mu.Lock()
	dest := ""
	for _, j := range core.State.Config.Jobs {
		if strings.HasPrefix(snapA, j.Dest) { dest = j.Dest; break }
	}
	core.State.Mu.Unlock()
	
	if dest == "" { http.Error(w, "Unknown snapshot path", 403); return }

	// Command: btrfs send --no-data -p <parent> <child> | btrfs receive --dump
	// Note: pipe handling in Go exec is verbose, simplified here using sh -c
	cmdStr := fmt.Sprintf("btrfs send --no-data -p '%s' '%s' | btrfs receive --dump", snapA, snapB)
	
	// Create a temporary log for this diff
	id := core.RunCommandAsync("DIFF", "🔍", "Compare", "sh", "-c", cmdStr)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

// --- Rollback Feature ---
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

	// Safety check
	if !strings.HasPrefix(snapPath, job.Dest) { http.Error(w, "Invalid snap path", 403); return }

	go func() {
		// 1. Rename live source to _backup_before_rollback
		backupName := fmt.Sprintf("%s_BEFORE_ROLLBACK_%d", job.Source, time.Now().Unix())
		core.RunCommandAsync("ROLLBACK", "📦", job.Source, "mv", job.Source, backupName)
		
		// 2. Snapshot the backup TO the source location (Read-Write)
		// Note: btrfs sub snap <snap> <live_loc>
		time.Sleep(1 * time.Second) // wait for mv
		core.RunCommandAsync("ROLLBACK", "♻️", job.Source, "btrfs", "subvolume", "snapshot", snapPath, job.Source)
	}()

	json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})
}

// --- Core Snapshot Logic (with Hooks & Replication) ---
func PerformBackupJob(job config.BackupJob) {
	os.MkdirAll(job.Dest, 0755)
	now := time.Now()
	name := now.Format(TimeLayout)
	fullDest := filepath.Join(job.Dest, name)
	
	core.PrintConsole(config.LogLevelDefault, "Starting Job: %s", job.Name)

	// 1. Pre-Hook
	if job.PreCommand != "" {
		core.RunCommandAsync("HOOK-PRE", "🪝", job.Name, "sh", "-c", job.PreCommand)
		time.Sleep(1 * time.Second) // Give it a sec
	}

	// 2. Snapshot
	cmd := exec.Command("btrfs", "subvolume", "snapshot", "-r", job.Source, fullDest)
	out, err := cmd.CombinedOutput()
	status := "Success"
	if err != nil { status = "Failed" }
	
	// Log it
	core.State.Mu.Lock()
	core.State.History = append([]core.LogEntry{{
		ID: time.Now().UnixNano(), Type: "SNAPSHOT", Emoji: "📸", 
		Path: fullDest, Timestamp: now.Format(time.RFC3339), 
		Status: status, Output: string(out), Duration: "0s",
	}}, core.State.History...)
	core.State.Mu.Unlock()
	core.SaveState()

	if status == "Success" {
		// 3. Replication (Simple Send/Receive)
		if job.Replication.Enabled && job.Replication.TargetDest != "" {
			// Needs a parent for incremental, but for simplicity here we do full send
			// In production, you'd find the previous snap in Dest to use as -p
			replCmd := fmt.Sprintf("btrfs send '%s' | btrfs receive '%s'", fullDest, job.Replication.TargetDest)
			core.RunCommandAsync("REPLICATE", "🚀", job.Replication.TargetDest, "sh", "-c", replCmd)
		}

		// 4. Post-Hook
		if job.PostCommand != "" {
			core.RunCommandAsync("HOOK-POST", "🪝", job.Name, "sh", "-c", job.PostCommand)
		}

		// 5. Retention
		EnforceRetention(job.Dest, job.Retention)
	}
}

func EnforceRetention(destPath string, cfg config.RetentionConfig) {
	if !cfg.Enabled { return }
	entries, _ := os.ReadDir(destPath)
	
	type Snap struct { Name string; Time time.Time }
	var snaps []Snap
	for _, e := range entries {
		if t, err := time.Parse(TimeLayout, e.Name()); err == nil {
			snaps = append(snaps, Snap{Name: e.Name(), Time: t})
		}
	}
	// Sort newest first
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })

	var toDel []string
	if cfg.Mode == "count" && len(snaps) > cfg.Value {
		for _, s := range snaps[cfg.Value:] { toDel = append(toDel, s.Name) }
	}
	
	// Delete
	for _, name := range toDel {
		p := filepath.Join(destPath, name)
		exec.Command("btrfs", "subvolume", "delete", p).Run()
	}
	if len(toDel) > 0 {
		core.PrintConsole(config.LogLevelDefault, "Retention deleted %d snapshots in %s", len(toDel), destPath)
	}
}
