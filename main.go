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
	"sort"
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

	// Handlers
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/logs/clear", handleClearLogs)
	
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

func runCommandAsync(opType, emoji, path, cmdName string, args ...string) int64 {
	state.mu.Lock()
	startTime := time.Now()
	entryID := time.Now().UnixNano()
	
	entry := LogEntry{
		ID:        entryID,
		Type:      opType,
		Emoji:     emoji,
		Path:      path,
		Timestamp: startTime.Format("02-01-2006 15:04 MST"),
		Status:    "Running...",
		Output:    fmt.Sprintf("Command: %s %s", cmdName, strings.Join(args, " ")),
	}
	state.History = append([]LogEntry{entry}, state.History...)
	state.mu.Unlock()

	go func() {
		cmd := exec.Command(cmdName, args...)
		output, err := cmd.CombinedOutput()
		duration := time.Since(startTime).Round(time.Millisecond)

		state.mu.Lock()
		defer state.mu.Unlock()
		
		for i, e := range state.History {
			if e.ID == entryID {
				state.History[i].Duration = duration.String()
				state.History[i].Output = string(output)
				
				// Handle specific exit codes
				if err != nil {
					// Check for "Operation in progress" (Exit code 1 + specific text)
					if strings.Contains(string(output), "Operation in progress") || strings.Contains(string(output), "inprogress") {
						state.History[i].Status = "Warning" // Mark as warning, not failure
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
		// Start
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

		entries, _ := os.ReadDir(dest)
		count := 0
		for _, e := range entries {
			if e.IsDir() {
				_, err := time.Parse(timeLayout, e.Name())
				if err == nil {
					exec.Command("btrfs", "subvolume", "delete", fmt.Sprintf("%s/%s", dest, e.Name())).Run()
					count++
				}
			}
		}
		runCommandAsync("PURGE ALL", "🔥", dest, "echo", fmt.Sprintf("Deleted %d snapshots", count))
	}()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	state.History = []LogEntry{}
	state.mu.Unlock()
	saveState()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "cleared"})
}

// --- Logic ---

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

	cmd := exec.Command("btrfs", "subvolume", "snapshot", "-r", src, fullDest)
	output, err := cmd.CombinedOutput()

	status := "Success"
	details := string(output)
	if err != nil {
		status = "Failed"
		details = fmt.Sprintf("%s : %s", err.Error(), string(output))
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
		count := 0
		for _, name := range toDelete {
			p := fmt.Sprintf("%s/%s", destPath, name)
			if err := exec.Command("btrfs", "subvolume", "delete", p).Run(); err == nil {
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
			if cfg.Unit == "hours" { unit = "h" }
			if cfg.Unit == "days" { unit = "d" }
			spec = fmt.Sprintf("@every %s%s", cfg.Value, unit)
		}
		id, _ := state.cron.AddFunc(spec, job)
		state.cronIDs[name] = id
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
