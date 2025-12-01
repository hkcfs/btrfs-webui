package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

//go:embed static/*
var content embed.FS

// --- Configuration Structs ---

type ScheduleConfig struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Unit    string `json:"unit"`
}

type RetentionConfig struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Value   int    `json:"value"`
	Unit    string `json:"unit"`
}

type Config struct {
	TargetDrive    string          `json:"target_drive"`
	SnapshotSource string          `json:"snapshot_source"`
	SnapshotDest   string          `json:"snapshot_dest"`
	SnapshotSched  ScheduleConfig  `json:"snapshot_sched"`
	ScrubSched     ScheduleConfig  `json:"scrub_sched"`
	BalanceSched   ScheduleConfig  `json:"balance_sched"`
	Retention      RetentionConfig `json:"retention"`
}

type LogEntry struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Emoji     string `json:"emoji"`
	Path      string `json:"path"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Output    string `json:"output"`
	Duration  string `json:"duration"`
}

type AppState struct {
	Config  Config     `json:"config"`
	History []LogEntry `json:"history"`
	mu      sync.Mutex
	cron    *cron.Cron
	cronIDs map[string]cron.EntryID
}

var state = AppState{
	cron:    cron.New(),
	cronIDs: make(map[string]cron.EntryID),
	Config: Config{
		SnapshotSched: ScheduleConfig{Unit: "minutes"},
		Retention:     RetentionConfig{Unit: "days", Mode: "count", Value: 5},
	},
}

const timeLayout = "02-01-2006-15-04-MST"

func main() {
	loadState()
	state.cron.Start()
	refreshSchedules()

	// Check for missed snapshots on startup
	go checkMissedSnapshots()

	// Handlers
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/logs/clear", handleClearLogs)
	
	// Snapshot Management
	http.HandleFunc("/api/snapshots/list", handleListSnapshots)
	http.HandleFunc("/api/snapshots/delete", handleDeleteSnapshot)

	// Actions
	http.HandleFunc("/api/action/snapshot", handleActionSnapshot)
	http.HandleFunc("/api/action/scrub", handleActionScrub)
	http.HandleFunc("/api/action/balance", handleActionBalance)
	http.HandleFunc("/api/action/defrag", handleActionDefrag)
	http.HandleFunc("/api/action/compsize", handleActionCompsize)
	http.HandleFunc("/api/action/purge_all", handlePurgeAllSnapshots)

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }

	fmt.Printf("🚀 BTRFS Manager started on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- Helper: Command Runner & Logger ---

func printDockerLog(opType, msg string, args ...interface{}) {
	timestamp := time.Now().Format(time.RFC3339)
	formattedMsg := fmt.Sprintf(msg, args...)
	fmt.Printf("[%s] [%s] %s\n", timestamp, opType, formattedMsg)
}

func runCommandAsync(opType, emoji, path, cmdName string, args ...string) int64 {
	state.mu.Lock()
	startTime := time.Now()
	entryID := time.Now().UnixNano()
	
	cmdStr := fmt.Sprintf("%s %s", cmdName, strings.Join(args, " "))

	entry := LogEntry{
		ID:        entryID,
		Type:      opType,
		Emoji:     emoji,
		Path:      path,
		Timestamp: startTime.Format("02-01-2006 15:04 MST"),
		Status:    "Running...",
		Output:    fmt.Sprintf("Command: %s", cmdStr),
	}
	state.History = append([]LogEntry{entry}, state.History...)
	state.mu.Unlock()

	go func() {
		printDockerLog(opType, "STARTING: %s", cmdStr)

		cmd := exec.Command(cmdName, args...)
		output, err := cmd.CombinedOutput()
		duration := time.Since(startTime).Round(time.Millisecond)
		outputStr := string(output)

		printDockerLog(opType, "FINISHED in %s", duration)
		if len(outputStr) > 0 {
			printDockerLog(opType, "OUTPUT:\n%s", outputStr)
		}
		if err != nil {
			printDockerLog(opType, "ERROR: %v", err)
		}
		fmt.Println("---------------------------------------------------------------")

		state.mu.Lock()
		defer state.mu.Unlock()
		
		for i, e := range state.History {
			if e.ID == entryID {
				state.History[i].Duration = duration.String()
				state.History[i].Output = outputStr
				
				if err != nil {
					if strings.Contains(outputStr, "Operation in progress") || strings.Contains(outputStr, "inprogress") {
						state.History[i].Status = "Warning" 
						state.History[i].Output += "\n\n⚠️ NOTE: A scrub/balance is already running in the background."
					} else {
						state.History[i].Status = "Failed"
						state.History[i].Output += fmt.Sprintf("\nError: %v", err)
					}
				} else {
					state.History[i].Status = "Success"
				}
				break
			}
		}
		if len(state.History) > 100 { state.History = state.History[:100] }
		saveState()
	}()

	return entryID
}

// --- Snapshot List & Delete Handlers ---

type SnapshotItem struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

func handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if dest == "" {
		http.Error(w, "Destination not configured", 400)
		return
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var list []SnapshotItem
	for _, e := range entries {
		if e.IsDir() {
			displayDate := "Unknown"
			t, err := time.Parse(timeLayout, e.Name())
			if err == nil {
				displayDate = t.Format("Jan 02, 2006 15:04 MST")
			} else {
				info, _ := e.Info()
				displayDate = info.ModTime().Format("Jan 02, 2006 15:04 MST")
			}

			list = append(list, SnapshotItem{
				Name: e.Name(),
				Date: displayDate,
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name > list[j].Name
	})

	json.NewEncoder(w).Encode(list)
}

func handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Name required", 400)
		return
	}

	state.mu.Lock()
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	fullPath := filepath.Join(dest, name)
	if filepath.Dir(fullPath) != filepath.Clean(dest) {
		http.Error(w, "Invalid path", 403)
		return
	}

	runCommandAsync("DELETE SNAP", "🗑️", fullPath, "btrfs", "subvolume", "delete", fullPath)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}


// --- Action Handlers ---

func handleActionSnapshot(w http.ResponseWriter, r *http.Request) {
	go performSnapshot()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered", "message": "Snapshot initiated"})
}

func handleActionScrub(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	path := state.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }

	var id int64
	if action == "status" {
		id = runCommandAsync("SCRUB CHECK", "🩺", path, "btrfs", "scrub", "status", path)
	} else if action == "cancel" {
		id = runCommandAsync("SCRUB STOP", "🛑", path, "btrfs", "scrub", "cancel", path)
	} else {
		id = runCommandAsync("SCRUB START", "🧹", path, "btrfs", "scrub", "start", "-B", path)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handleActionBalance(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	path := state.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }

	var id int64
	if action == "status" {
		id = runCommandAsync("BALANCE CHECK", "⚖️", path, "btrfs", "balance", "status", path)
	} else if action == "cancel" {
		id = runCommandAsync("BALANCE STOP", "🛑", path, "btrfs", "balance", "cancel", path)
	} else {
		id = runCommandAsync("BALANCE START", "⚖️", path, "btrfs", "balance", "start", "--full-balance", path)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handleActionDefrag(w http.ResponseWriter, r *http.Request) {
	path := state.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }
	id := runCommandAsync("DEFRAG", "📦", path, "btrfs", "filesystem", "defragment", "-r", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handleActionCompsize(w http.ResponseWriter, r *http.Request) {
	path := state.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }
	id := runCommandAsync("COMPSIZE", "📊", path, "compsize", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handlePurgeAllSnapshots(w http.ResponseWriter, r *http.Request) {
	go func() {
		state.mu.Lock()
		dest := state.Config.SnapshotDest
		state.mu.Unlock()
		if dest == "" { return }

		printDockerLog("PURGE ALL", "Starting purge of %s", dest)

		entries, _ := os.ReadDir(dest)
		count := 0
		for _, e := range entries {
			if e.IsDir() {
				_, err := time.Parse(timeLayout, e.Name())
				if err == nil {
					p := fmt.Sprintf("%s/%s", dest, e.Name())
					printDockerLog("PURGE", "Deleting: %s", p)
					exec.Command("btrfs", "subvolume", "delete", p).Run()
					count++
				}
			}
		}
		
		msg := fmt.Sprintf("Deleted %d snapshots", count)
		printDockerLog("PURGE ALL", "Finished: %s", msg)
		runCommandAsync("PURGE ALL", "🔥", dest, "echo", msg)
	}()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	state.History = []LogEntry{}
	state.mu.Unlock()
	printDockerLog("SYSTEM", "Logs cleared by user")
	saveState()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "cleared"})
}

// --- Logic ---

func checkMissedSnapshots() {
	// Give the system a moment to settle
	time.Sleep(2 * time.Second)

	state.mu.Lock()
	sched := state.Config.SnapshotSched
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	// Only catch up if enabled and using "every_x" (cron is too ambiguous)
	if !sched.Enabled || dest == "" || sched.Type != "every_x" {
		return 
	}

	// Calculate config interval
	val, err := strconv.Atoi(sched.Value)
	if err != nil || val == 0 { val = 1 }
	
	var duration time.Duration
	switch sched.Unit {
	case "minutes":
		duration = time.Duration(val) * time.Minute
	case "hours":
		duration = time.Duration(val) * time.Hour
	case "days":
		duration = time.Duration(val) * 24 * time.Hour
	default:
		return
	}

	// Find last actual snapshot on disk
	entries, err := os.ReadDir(dest)
	if err != nil {
		printDockerLog("CATCHUP", "Cannot read destination to check missed snapshots: %v", err)
		return
	}

	var lastTime time.Time
	found := false

	for _, e := range entries {
		if e.IsDir() {
			t, err := time.Parse(timeLayout, e.Name())
			if err == nil {
				if t.After(lastTime) {
					lastTime = t
					found = true
				}
			}
		}
	}

	// If we found snapshots and the gap is larger than interval
	if found {
		gap := time.Since(lastTime)
		if gap > duration {
			printDockerLog("CATCHUP", "Missed schedule detected! Last snapshot was %s ago. Interval is %s. Triggering now.", gap, duration)
			performSnapshot()
		} else {
			printDockerLog("CATCHUP", "No missed snapshots. Last was %s ago.", gap)
		}
	} else {
		// Optional: If no snapshots exist at all, do we start now?
		// Usually yes for "Every X" schedules.
		printDockerLog("CATCHUP", "No existing snapshots found. Triggering initial snapshot.")
		performSnapshot()
	}
}

func performSnapshot() {
	state.mu.Lock()
	src := state.Config.SnapshotSource
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if src == "" || dest == "" { return }
	os.MkdirAll(dest, 0755)

	now := time.Now()
	name := now.Format(timeLayout)
	fullDest := fmt.Sprintf("%s/%s", strings.TrimRight(dest, "/"), name)
	visualPath := fmt.Sprintf("%s ➡️ %s", src, name)

	printDockerLog("SNAPSHOT", "Creating snapshot %s -> %s", src, fullDest)

	cmd := exec.Command("btrfs", "subvolume", "snapshot", "-r", src, fullDest)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if len(outputStr) > 0 {
		printDockerLog("SNAPSHOT", "Output:\n%s", outputStr)
	}
	if err != nil {
		printDockerLog("SNAPSHOT", "Error: %v", err)
	}

	status := "Success"
	details := outputStr
	if err != nil {
		status = "Failed"
		details = fmt.Sprintf("%s : %s", err.Error(), outputStr)
	}
	
	logHistory("SNAPSHOT", "📸", visualPath, status, details)

	if status == "Success" {
		enforceRetention(dest)
	}
}

func enforceRetention(destPath string) {
	state.mu.Lock()
	cfg := state.Config.Retention
	state.mu.Unlock()

	if !cfg.Enabled { return }

	entries, err := os.ReadDir(destPath)
	if err != nil { return }

	type SnapInfo struct {
		Name string
		Time time.Time
	}
	var snaps []SnapInfo

	for _, e := range entries {
		if !e.IsDir() { continue }
		t, err := time.Parse(timeLayout, e.Name())
		if err == nil {
			snaps = append(snaps, SnapInfo{Name: e.Name(), Time: t})
		}
	}

	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].Time.After(snaps[j].Time)
	})

	var toDelete []string

	if cfg.Mode == "count" {
		if len(snaps) > cfg.Value {
			for _, s := range snaps[cfg.Value:] {
				toDelete = append(toDelete, s.Name)
			}
		}
	} else if cfg.Mode == "time" {
		var cutoff time.Time
		now := time.Now()
		switch cfg.Unit {
		case "days": cutoff = now.AddDate(0, 0, -cfg.Value)
		case "weeks": cutoff = now.AddDate(0, 0, -cfg.Value*7)
		case "months": cutoff = now.AddDate(0, -cfg.Value, 0)
		case "years": cutoff = now.AddDate(-cfg.Value, 0, 0)
		default: cutoff = now.AddDate(0, 0, -cfg.Value)
		}

		for _, s := range snaps {
			if s.Time.Before(cutoff) {
				toDelete = append(toDelete, s.Name)
			}
		}
	}

	if len(toDelete) > 0 {
		printDockerLog("RETENTION", "Cleaning up %d old snapshots", len(toDelete))
		count := 0
		for _, name := range toDelete {
			p := fmt.Sprintf("%s/%s", destPath, name)
			if err := exec.Command("btrfs", "subvolume", "delete", p).Run(); err == nil {
				printDockerLog("RETENTION", "Deleted: %s", name)
				count++
			}
		}
		logHistory("RETENTION", "🗑️", destPath, "Success", fmt.Sprintf("Cleaned up %d old snapshots", count))
	}
}

func logHistory(opType, emoji, path, status, output string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	entry := LogEntry{
		ID:        time.Now().UnixNano(),
		Type:      opType,
		Emoji:     emoji,
		Path:      path,
		Timestamp: time.Now().Format("02-01-2006 15:04 MST"),
		Status:    status,
		Output:    output,
		Duration:  "0s",
	}
	state.History = append([]LogEntry{entry}, state.History...)
	if len(state.History) > 100 { state.History = state.History[:100] }
	saveState()
}

// --- Scheduler Logic ---

func refreshSchedules() {
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, id := range state.cronIDs { state.cron.Remove(id) }
	state.cronIDs = make(map[string]cron.EntryID)

	addJob := func(name string, cfg ScheduleConfig, job func()) {
		if !cfg.Enabled { return }
		spec := cfg.Value
		if cfg.Type == "every_x" {
			unit := "m"
			// Convert Days to Hours for compatibility
			if cfg.Unit == "days" {
				val, _ := strconv.Atoi(cfg.Value)
				// Overwrite spec value logic just for the cron registration
				// We don't change cfg.Value permanently to keep UI consistent
				spec = fmt.Sprintf("@every %dh", val * 24)
			} else {
				if cfg.Unit == "hours" { unit = "h" }
				spec = fmt.Sprintf("@every %s%s", cfg.Value, unit)
			}
		}
		id, err := state.cron.AddFunc(spec, job)
		if err == nil {
			printDockerLog("SCHEDULER", "Registered %s job: %s", name, spec)
			state.cronIDs[name] = id
		} else {
			printDockerLog("SCHEDULER", "Error registering %s: %v", name, err)
		}
	}

	addJob("snapshot", state.Config.SnapshotSched, func() { go performSnapshot() })
	addJob("scrub", state.Config.ScrubSched, func() {
		p := state.Config.TargetDrive
		if p != "" { runCommandAsync("AUTO SCRUB", "🧹", p, "btrfs", "scrub", "start", "-B", p) }
	})
	addJob("balance", state.Config.BalanceSched, func() {
		p := state.Config.TargetDrive
		if p != "" { runCommandAsync("AUTO BALANCE", "⚖️", p, "btrfs", "balance", "start", "--full-balance", p) }
	})
}

// --- HTTP Boilerplate ---

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, _ := template.ParseFS(content, "static/index.html")
	tmpl.Execute(w, nil)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if r.Method == "POST" {
		var newConfig Config
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err == nil {
			state.Config = newConfig
			saveState()
			go refreshSchedules()
		}
	}
	json.NewEncoder(w).Encode(state.Config)
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	defer state.mu.Unlock()
	json.NewEncoder(w).Encode(state.History)
}

func saveState() {
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile("/data/state.json", data, 0644)
}

func loadState() {
	data, err := os.ReadFile("/data/state.json")
	if err == nil {
		var loaded AppState
		json.Unmarshal(data, &loaded)
		state.Config = loaded.Config
		state.History = loaded.History
	}
}
