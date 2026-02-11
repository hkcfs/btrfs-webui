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
	"02-01-2006-15-04",       // Custom format (minus TZ)
	"02-01-2006-15-04-MST",   // Legacy support
	"2006-01-02-15-04-MST",   // ISO-like
	time.RFC3339,
}

// Robust Parser: Ignores text suffix if standard parse fails
func ParseSnapshotTime(e os.DirEntry) time.Time {
	name := e.Name()
	
	// 1. Try standard layouts
	for _, layout := range TimeLayouts {
		if t, err := time.ParseInLocation(layout, name, time.Local); err == nil {
			return t
		}
	}

	// 2. Try "Fuzzy" Parsing (Strip Timezone Suffix like -IST, -CET)
	// Format: DD-MM-YYYY-HH-mm-TZ
	// We want: DD-MM-YYYY-HH-mm
	parts := strings.Split(name, "-")
	if len(parts) >= 5 {
		// Reconstruct the date/time part only (first 5 parts)
		datePart := strings.Join(parts[:5], "-") 
		layout := "02-01-2006-15-04"
		if t, err := time.ParseInLocation(layout, datePart, time.Local); err == nil {
			return t
		}
	}

	// 3. Fallback to File Info (Modification Time)
	info, _ := e.Info()
	return info.ModTime()
}

// --- Snapshot Listing ---
func HandleListSnapshots(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleListSnapshots called for job_id=%s", jobID)
	
	var dest string
	core.State.Mu.Lock()
	core.PrintConsole(config.LogLevelVerbose, "[API] Looking up job in %d configured jobs", len(core.State.Config.Jobs))
	for _, j := range core.State.Config.Jobs {
		if j.ID == jobID { 
			dest = j.Dest 
			core.PrintConsole(config.LogLevelVerbose, "[API] Found job: name=%s, dest=%s", j.Name, j.Dest)
			break 
		}
	}
	core.State.Mu.Unlock()

	if dest == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[API] Job not found: job_id=%s", jobID)
		http.Error(w, "Job not found", 404) 
		return 
	}

	core.PrintConsole(config.LogLevelVerbose, "[API] Reading directory: %s", dest)
	entries, err := os.ReadDir(dest)
	if err != nil { 
		core.PrintConsole("ERROR", "ReadDir failed: %v", err)
		http.Error(w, err.Error(), 500)
		return 
	}
	core.PrintConsole(config.LogLevelVerbose, "[API] Found %d entries in destination", len(entries))

	type SnapshotItem struct {
		Name    string    `json:"name"`
		Date    string    `json:"date"`
		SortKey time.Time `json:"-"`
		Path    string    `json:"path"`
		Locked  bool      `json:"locked"`
	}
	var list []SnapshotItem
	
	for _, e := range entries {
		if e.IsDir() {
			core.PrintConsole(config.LogLevelVerbose, "[API] Processing snapshot: %s", e.Name())
			t := ParseSnapshotTime(e)
			fullPath := filepath.Join(dest, e.Name())
			locked := isLocked(fullPath)
			core.PrintConsole(config.LogLevelVerbose, "[API] Snapshot %s: time=%s, locked=%v", e.Name(), t.Format(time.RFC3339), locked)
			list = append(list, SnapshotItem{
				Name:    e.Name(),
				Date:    t.Format("Jan 02, 2006 15:04 MST"),
				SortKey: t,
				Path:    fullPath,
				Locked:  locked,
			})
		}
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[API] Sorting %d snapshots by date", len(list))
	sort.Slice(list, func(i, j int) bool { return list[i].SortKey.After(list[j].SortKey) })
	core.PrintConsole(config.LogLevelVerbose, "[API] Returning %d snapshots", len(list))
	json.NewEncoder(w).Encode(list)
}

// --- Job Stats ---
func HandleJobStats(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleJobStats called for job_id=%s", jobID)
	
	var dest string
	core.State.Mu.Lock()
	for _, j := range core.State.Config.Jobs {
		if j.ID == jobID { 
			dest = j.Dest
			core.PrintConsole(config.LogLevelVerbose, "[API] Found job: %s, dest=%s", j.Name, j.Dest)
			break 
		}
	}
	core.State.Mu.Unlock()

	if dest == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[API] Job not found: %s", jobID)
		http.Error(w, "Job not found", 404)
		return
	}

	core.PrintConsole(config.LogLevelVerbose, "[API] Reading stats from: %s", dest)
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

	core.PrintConsole(config.LogLevelVerbose, "[API] Stats for job %s: count=%d, oldest=%s", jobID, count, oldestStr)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": count,
		"oldest": oldestStr,
	})
}

// --- Diff & Rollback ---
func HandleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
	snapA := r.URL.Query().Get("a")
	snapB := r.URL.Query().Get("b")
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleSnapshotDiff called: snapA=%s, snapB=%s", snapA, snapB)
	
	if snapA == "" || snapB == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[DIFF] Error: Missing snapshot path(s)")
		http.Error(w, "Need two snapshots", 400)
		return
	}

	core.State.Mu.Lock()
	dest := ""
	for _, j := range core.State.Config.Jobs {
		if strings.HasPrefix(snapA, j.Dest) { 
			dest = j.Dest
			core.PrintConsole(config.LogLevelVerbose, "[DIFF] Authorized by job: %s", j.Name)
			break 
		}
	}
	core.State.Mu.Unlock()
	
	if dest == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[DIFF] Access denied: path not in any job destination")
		http.Error(w, "Unknown snapshot path", 403)
		return
	}

	cmdStr := fmt.Sprintf("btrfs send --no-data -p '%s' '%s' | btrfs receive --dump", snapA, snapB)
	core.PrintConsole(config.LogLevelVerbose, "[DIFF] Command: %s", cmdStr)
	id := core.RunCommandAsync("DIFF", "🔍", "Compare", "sh", "-c", cmdStr)
	core.PrintConsole(config.LogLevelVerbose, "[DIFF] Diff job ID: %d", id)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

func HandleRollback(w http.ResponseWriter, r *http.Request) {
	snapPath := r.URL.Query().Get("path")
	jobID := r.URL.Query().Get("job_id")
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleRollback called: snapPath=%s, jobID=%s", snapPath, jobID)
	
	var job config.BackupJob
	found := false
	core.State.Mu.Lock()
	for _, j := range core.State.Config.Jobs {
		if j.ID == jobID { 
			job = j
			found = true
			core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Found job: %s", j.Name)
			break 
		}
	}
	core.State.Mu.Unlock()

	if !found { 
		core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Job not found: %s", jobID)
		http.Error(w, "Job not found", 404)
		return
	}
	
	if !strings.HasPrefix(snapPath, job.Dest) { 
		core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Invalid path: %s not in job dest %s", snapPath, job.Dest)
		http.Error(w, "Invalid snap path", 403)
		return
	}

	core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Initiating rollback: %s -> %s", snapPath, job.Source)
	go func() {
		backupName := fmt.Sprintf("%s_BEFORE_ROLLBACK_%d", job.Source, time.Now().Unix())
		core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Step 1: Creating backup at %s", backupName)
		core.RunCommandAsync("ROLLBACK", "📦", job.Source, "mv", job.Source, backupName)
		core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Waiting 1 second...")
		time.Sleep(1 * time.Second)
		core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Step 2: Restoring snapshot from %s to %s", snapPath, job.Source)
		core.RunCommandAsync("ROLLBACK", "♻️", job.Source, "btrfs", "subvolume", "snapshot", snapPath, job.Source)
		core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Rollback complete")
	}()

	json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})
	core.PrintConsole(config.LogLevelVerbose, "[ROLLBACK] Rollback triggered successfully")
}

func HandleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleDeleteSnapshot called: path=%s", path)
	
	if path == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[DELETE] Error: Path is empty")
		http.Error(w, "Path required", 400) 
		return 
	}
	
	allowed := false
	core.State.Mu.Lock()
	core.PrintConsole(config.LogLevelVerbose, "[DELETE] Checking authorization against %d jobs", len(core.State.Config.Jobs))
	for _, j := range core.State.Config.Jobs {
		if strings.HasPrefix(filepath.Clean(path), filepath.Clean(j.Dest)) {
			allowed = true
			core.PrintConsole(config.LogLevelVerbose, "[DELETE] Path authorized by job: %s", j.Name)
			break
		}
	}
	core.State.Mu.Unlock()
	
	if !allowed { 
		core.PrintConsole(config.LogLevelVerbose, "[DELETE] Access forbidden: path %s", path)
		http.Error(w, "Forbidden", 403) 
		return 
	}

	core.PrintConsole(config.LogLevelVerbose, "[DELETE] Triggering async deletion for: %s", path)
	core.RunCommandAsync("DELETE SNAP", "🗑", path, "btrfs", "subvolume", "delete", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
	core.PrintConsole(config.LogLevelVerbose, "[DELETE] Deletion triggered successfully")
}

// --- Core Snapshot Logic ---
func PerformBackupJob(job config.BackupJob) {
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] ====== Starting Backup Job ======")
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Job ID: %s", job.ID)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Job Name: %s", job.Name)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Source: %s", job.Source)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Destination: %s", job.Dest)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Schedule: enabled=%v, type=%s, value=%s, unit=%s", 
		job.Schedule.Enabled, job.Schedule.Type, job.Schedule.Value, job.Schedule.Unit)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Retention: enabled=%v, mode=%s, value=%d, unit=%s",
		job.Retention.Enabled, job.Retention.Mode, job.Retention.Value, job.Retention.Unit)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Replication: enabled=%v, target=%s",
		job.Replication.Enabled, job.Replication.TargetDest)

	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Creating destination directory: %s", job.Dest)
	os.MkdirAll(job.Dest, 0755)
	
	now := time.Now()
	tz, _ := now.Zone()
	name := fmt.Sprintf("%s-%s", now.Format("02-01-2006-15-04"), tz)
	fullDest := filepath.Join(job.Dest, name)
	
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Generated snapshot name: %s", name)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Full destination path: %s", fullDest)
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Timestamp: %s, Timezone: %s", now.Format(time.RFC3339), tz)
	
	core.PrintConsole(config.LogLevelDefault, "Starting Job: %s", job.Name)

	if job.PreCommand != "" {
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Executing pre-hook command: %s", job.PreCommand)
		core.RunCommandAsync("HOOK-PRE", "🪝", job.Name, "sh", "-c", job.PreCommand)
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Waiting 1 second for pre-hook to complete...")
		time.Sleep(1 * time.Second)
	} else {
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] No pre-hook configured")
	}

	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Creating BTRFS read-only snapshot...")
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Command: btrfs subvolume snapshot -r %s %s", job.Source, fullDest)
	
	cmd := exec.Command("btrfs", "subvolume", "snapshot", "-r", job.Source, fullDest)
	out, err := cmd.CombinedOutput()
	
	status := "Success"
	if err != nil { 
		status = "Failed" 
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Snapshot creation FAILED: %v", err)
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Error output: %s", string(out))
	} else {
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Snapshot creation SUCCESS")
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Command output: %s", string(out))
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Updating history with snapshot result...")
	core.State.Mu.Lock()
	core.State.History = append([]core.LogEntry{{
		ID: time.Now().UnixNano(), Type: "SNAPSHOT", Emoji: "📸", 
		Path: fullDest, Timestamp: now.Format(time.RFC3339), 
		Status: status, Output: string(out), Duration: "0s",
	}}, core.State.History...)
	core.State.Mu.Unlock()
	core.SaveState()
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] State saved to disk")

	if status == "Success" {
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Snapshot successful, proceeding with post-processing...")
		
		if job.Replication.Enabled && job.Replication.TargetDest != "" {
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Replication enabled, target: %s", job.Replication.TargetDest)
			replCmd := fmt.Sprintf("btrfs send '%s' | btrfs receive '%s'", fullDest, job.Replication.TargetDest)
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Replication command: %s", replCmd)
			core.RunCommandAsync("REPLICATE", "🚀", job.Replication.TargetDest, "sh", "-c", replCmd)
		} else {
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Replication disabled or no target configured")
		}
		
		if job.PostCommand != "" {
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Executing post-hook command: %s", job.PostCommand)
			core.RunCommandAsync("HOOK-POST", "🪝", job.Name, "sh", "-c", job.PostCommand)
		} else {
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] No post-hook configured")
		}
		
		if job.Retention.Enabled {
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Enforcing retention policy...")
			EnforceRetention(job.Dest, job.Retention)
		} else {
			core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Retention policy disabled")
		}
	} else {
		core.PrintConsole(config.LogLevelVerbose, "[BACKUP] Snapshot failed, skipping post-processing")
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[BACKUP] ====== Backup Job Complete ======")
}

func isLocked(path string) bool {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	locked := core.State.LockedSnaps[path]
	core.PrintConsole(config.LogLevelVerbose, "[LOCK] Checking lock status for: %s = %v", path, locked)
	return locked
}

func HandleToggleLock(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	lock := r.URL.Query().Get("lock") == "true"
	
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleToggleLock called: path=%s, lock=%v", path, lock)
	
	if path == "" {
		core.PrintConsole(config.LogLevelVerbose, "[LOCK] Error: Path is empty")
		http.Error(w, "Path required", 400)
		return
	}

	allowed := false
	core.State.Mu.Lock()
	core.PrintConsole(config.LogLevelVerbose, "[LOCK] Checking authorization against %d jobs", len(core.State.Config.Jobs))
	for _, j := range core.State.Config.Jobs {
		if strings.HasPrefix(filepath.Clean(path), filepath.Clean(j.Dest)) {
			allowed = true
			core.PrintConsole(config.LogLevelVerbose, "[LOCK] Path authorized by job: %s", j.Name)
			break
		}
	}
	
	if !allowed {
		core.State.Mu.Unlock()
		core.PrintConsole(config.LogLevelVerbose, "[LOCK] Access forbidden: path %s not in any job destination", path)
		http.Error(w, "Forbidden", 403)
		return
	}

	action := "unlocking"
	if lock {
		action = "locking"
		core.State.LockedSnaps[path] = true
	} else {
		delete(core.State.LockedSnaps, path)
	}
	
	core.SaveState()
	core.State.Mu.Unlock()

	core.PrintConsole(config.LogLevelVerbose, "[LOCK] Successfully %s snapshot: %s", action, path)
	core.PrintConsole(config.LogLevelVerbose, "[LOCK] Total locked snapshots: %d", len(core.State.LockedSnaps))
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "locked": lock})
}

func EnforceRetention(destPath string, cfg config.RetentionConfig) {
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] ====== Enforcing Retention Policy ======")
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Destination: %s", destPath)
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Config: enabled=%v, mode=%s, value=%d, unit=%s",
		cfg.Enabled, cfg.Mode, cfg.Value, cfg.Unit)

	if !cfg.Enabled {
		core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Retention policy is disabled, skipping")
		return
	}

	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Reading directory: %s", destPath)
	entries, err := os.ReadDir(destPath)
	if err != nil {
		core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Failed to read directory: %v", err)
		return
	}
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Found %d entries", len(entries))

	type Snap struct {
		Name   string
		Time   time.Time
		Locked bool
	}
	var snaps []Snap
	
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Analyzing snapshots...")
	for _, e := range entries {
		if e.IsDir() {
			t := ParseSnapshotTime(e)
			p := filepath.Join(destPath, e.Name())
			locked := isLocked(p)
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Snapshot: %s, time=%s, user-locked=%v",
				e.Name(), t.Format(time.RFC3339), locked)
			snaps = append(snaps, Snap{Name: e.Name(), Time: t, Locked: locked})
		}
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Sorting %d snapshots by date (newest first)", len(snaps))
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })

	var toDel []string
	
	if cfg.Mode == "count" {
		core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Mode: COUNT - Keeping last %d snapshots", cfg.Value)
		count := 0
		for i, s := range snaps {
			if s.Locked {
				core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d] %s is LOCKED (read-only), skipping", i, s.Name)
				continue
			}
			count++
			if count > cfg.Value {
				core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d] %s exceeds count limit (%d), marking for deletion",
					i, s.Name, cfg.Value)
				toDel = append(toDel, s.Name)
			} else {
				core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d] %s within limit (position %d/%d), keeping",
					i, s.Name, count, cfg.Value)
			}
		}
	} else if cfg.Mode == "time" {
		var cutoff time.Time
		now := time.Now()
		switch cfg.Unit {
		case "days":
			cutoff = now.AddDate(0, 0, -cfg.Value)
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Mode: TIME - Keeping snapshots newer than %d days (cutoff: %s)",
				cfg.Value, cutoff.Format(time.RFC3339))
		case "weeks":
			cutoff = now.AddDate(0, 0, -cfg.Value*7)
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Mode: TIME - Keeping snapshots newer than %d weeks (cutoff: %s)",
				cfg.Value, cutoff.Format(time.RFC3339))
		case "months":
			cutoff = now.AddDate(0, -cfg.Value, 0)
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Mode: TIME - Keeping snapshots newer than %d months (cutoff: %s)",
				cfg.Value, cutoff.Format(time.RFC3339))
		case "years":
			cutoff = now.AddDate(-cfg.Value, 0, 0)
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Mode: TIME - Keeping snapshots newer than %d years (cutoff: %s)",
				cfg.Value, cutoff.Format(time.RFC3339))
		}
		
		for i, s := range snaps {
			if s.Locked {
				core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d] %s is LOCKED (read-only), skipping",
					i, s.Name)
				continue
			}
			if s.Time.Before(cutoff) {
				core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d] %s is OLDER than cutoff (%s), marking for deletion",
					i, s.Name, s.Time.Format(time.RFC3339))
				toDel = append(toDel, s.Name)
			} else {
				core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d] %s is NEWER than cutoff (%s), keeping",
					i, s.Name, s.Time.Format(time.RFC3339))
			}
		}
	}

	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Marked %d snapshots for deletion", len(toDel))
	
	for i, name := range toDel {
		p := filepath.Join(destPath, name)
		core.PrintConsole(config.LogLevelVerbose, "[RETENTION] [%d/%d] Deleting: %s", i+1, len(toDel), p)
		err := exec.Command("btrfs", "subvolume", "delete", p).Run()
		if err != nil {
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Failed to delete %s: %v", name, err)
		} else {
			core.PrintConsole(config.LogLevelVerbose, "[RETENTION] Successfully deleted %s", name)
		}
	}
	
	if len(toDel) > 0 {
		core.PrintConsole(config.LogLevelDefault, "Retention deleted %d snapshots in %s", len(toDel), destPath)
	} else {
		core.PrintConsole(config.LogLevelVerbose, "[RETENTION] No snapshots to delete")
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[RETENTION] ====== Retention Complete ======")
}
