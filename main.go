package main

import (
	"bufio"
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
	Config   Config            `json:"config"`
	Profiles map[string]Config `json:"profiles"` // NEW: Store saved configs
	History  []LogEntry        `json:"history"`
	mu       sync.Mutex
	cron     *cron.Cron
	cronIDs  map[string]cron.EntryID
}

var state = AppState{
	cron:     cron.New(),
	cronIDs:  make(map[string]cron.EntryID),
	Profiles: make(map[string]Config),
	Config: Config{
		SnapshotSched: ScheduleConfig{Unit: "minutes"},
		ScrubSched:    ScheduleConfig{Unit: "days"},
		BalanceSched:  ScheduleConfig{Unit: "days"},
		Retention:     RetentionConfig{Unit: "days", Mode: "count", Value: 5},
	},
}

const timeLayout = "02-01-2006-15-04-MST"

func main() {
	loadState()
	state.cron.Start()
	refreshSchedules()

	go checkMissedSnapshots()

	// Handlers
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/logs/clear", handleClearLogs)

	// Profile Management (NEW)
	http.HandleFunc("/api/profiles/list", handleProfileList)
	http.HandleFunc("/api/profiles/save", handleProfileSave)
	http.HandleFunc("/api/profiles/load", handleProfileLoad)
	http.HandleFunc("/api/profiles/delete", handleProfileDelete)

	// Features
	http.HandleFunc("/api/storage/usage", handleStorageUsage)
	http.HandleFunc("/api/health/smart", handleSmartData)
	http.HandleFunc("/api/health/test", handleSmartTest)
	http.HandleFunc("/api/health/btrfs", handleBtrfsStats)
	http.HandleFunc("/api/browser/list", handleBrowserList)
	http.HandleFunc("/api/browser/download", handleBrowserDownload)

	// Actions
	http.HandleFunc("/api/snapshots/list", handleListSnapshots)
	http.HandleFunc("/api/snapshots/delete", handleDeleteSnapshot)
	http.HandleFunc("/api/action/snapshot", handleActionSnapshot)
	http.HandleFunc("/api/action/scrub", handleActionScrub)
	http.HandleFunc("/api/action/balance", handleActionBalance)
	http.HandleFunc("/api/action/defrag", handleActionDefrag)
	http.HandleFunc("/api/action/compsize", handleActionCompsize)
	http.HandleFunc("/api/action/purge_all", handlePurgeAllSnapshots)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 BTRFS Manager started on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- NEW PROFILE HANDLERS ---

func handleProfileList(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	defer state.mu.Unlock()
	keys := make([]string, 0, len(state.Profiles))
	for k := range state.Profiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	json.NewEncoder(w).Encode(keys)
}

func handleProfileSave(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Name required", 400)
		return
	}
	state.mu.Lock()
	state.Profiles[name] = state.Config
	state.mu.Unlock()
	saveState()
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleProfileLoad(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	state.mu.Lock()
	if cfg, ok := state.Profiles[name]; ok {
		state.Config = cfg
		state.mu.Unlock()
		saveState()
		refreshSchedules() // Apply the loaded schedule
		json.NewEncoder(w).Encode(map[string]string{"status": "loaded"})
	} else {
		state.mu.Unlock()
		http.Error(w, "Profile not found", 404)
	}
}

func handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	state.mu.Lock()
	delete(state.Profiles, name)
	state.mu.Unlock()
	saveState()
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// --- Helper: Command Runner & Logger ---

func printDockerLog(opType, msg string, args ...interface{}) {
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Printf("[%s] [%s] %s\n", timestamp, opType, fmt.Sprintf(msg, args...))
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
		if err != nil {
			printDockerLog(opType, "ERROR: %v", err)
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		for i, e := range state.History {
			if e.ID == entryID {
				state.History[i].Duration = duration.String()
				state.History[i].Output = outputStr
				if err != nil {
					if strings.Contains(outputStr, "Operation in progress") || strings.Contains(outputStr, "inprogress") {
						state.History[i].Status = "Warning"
						state.History[i].Output += "\n\n⚠️ NOTE: A background operation is already running."
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
		if len(state.History) > 100 {
			state.History = state.History[:100]
		}
		saveState()
	}()
	return entryID
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
	if len(state.History) > 100 {
		state.History = state.History[:100]
	}
	saveState()
}

// --- NEW FEATURE HANDLERS ---

func handleStorageUsage(w http.ResponseWriter, r *http.Request) {
	path := state.Config.TargetDrive
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}

	out, err := exec.Command("btrfs", "filesystem", "usage", "-b", path).Output()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	text := string(out)
	parseBytes := func(key string) int64 {
		scanner := bufio.NewScanner(strings.NewReader(text))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, key) {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					val, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
					return val
				}
			}
		}
		return 0
	}

	resp := map[string]int64{
		"device_size":        parseBytes("Device size:"),
		"device_allocated":   parseBytes("Device allocated:"),
		"device_unallocated": parseBytes("Device unallocated:"),
		"used":               parseBytes("Used:"),
		"free":               parseBytes("Free (estimated):"),
	}
	json.NewEncoder(w).Encode(resp)
}

func handleSmartData(w http.ResponseWriter, r *http.Request) {
	path := state.Config.TargetDrive
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}

	dfOut, _ := exec.Command("df", path).Output()
	lines := strings.Split(strings.TrimSpace(string(dfOut)), "\n")
	if len(lines) < 2 {
		http.Error(w, "Could not resolve device", 500)
		return
	}

	device := strings.Fields(lines[1])[0]
	baseDevice := strings.TrimRight(device, "0123456789")

	cmd := exec.Command("smartctl", "-a", "-j", baseDevice)
	out, _ := cmd.CombinedOutput()
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func handleSmartTest(w http.ResponseWriter, r *http.Request) {
	testType := r.URL.Query().Get("type")
	path := state.Config.TargetDrive

	dfOut, _ := exec.Command("df", path).Output()
	lines := strings.Split(strings.TrimSpace(string(dfOut)), "\n")
	device := strings.Fields(lines[1])[0]
	baseDevice := strings.TrimRight(device, "0123456789")

	id := runCommandAsync("SMART TEST", "🩺", baseDevice, "smartctl", "-t", testType, baseDevice)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handleBtrfsStats(w http.ResponseWriter, r *http.Request) {
	path := state.Config.TargetDrive
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}
	out, err := exec.Command("btrfs", "device", "stats", path).CombinedOutput()
	if err != nil {
		http.Error(w, string(out), 500)
		return
	}

	stats := make(map[string]map[string]int)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 2 {
			dev := strings.TrimSuffix(strings.TrimPrefix(parts[0], "["), "].")
			key := parts[0][strings.LastIndex(parts[0], ".")+1:]
			val, _ := strconv.Atoi(parts[1])
			if _, ok := stats[dev]; !ok {
				stats[dev] = make(map[string]int)
			}
			stats[dev][key] = val
		}
	}
	json.NewEncoder(w).Encode(stats)
}

func handleBrowserList(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		http.Error(w, "Path required", 400)
		return
	}

	state.mu.Lock()
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if dest == "" || !strings.HasPrefix(filepath.Clean(reqPath), filepath.Clean(dest)) {
		http.Error(w, "Access Denied", 403)
		return
	}

	entries, err := os.ReadDir(reqPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	type FileItem struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
		Path  string `json:"path"`
	}
	var files []FileItem
	for _, e := range entries {
		info, _ := e.Info()
		files = append(files, FileItem{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			Path:  filepath.Join(reqPath, e.Name()),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})
	json.NewEncoder(w).Encode(files)
}

func handleBrowserDownload(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	state.mu.Lock()
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if dest == "" || !strings.HasPrefix(filepath.Clean(reqPath), filepath.Clean(dest)) {
		http.Error(w, "Access Denied", 403)
		return
	}
	http.ServeFile(w, r, reqPath)
}

// --- Original Snapshot Logic ---

type SnapshotItem struct {
	Name    string    `json:"name"`
	Date    string    `json:"date"`
	SortKey time.Time `json:"-"`
	Path    string    `json:"path"`
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
			t, err := time.Parse(timeLayout, e.Name())
			if err != nil {
				info, _ := e.Info()
				t = info.ModTime()
			}
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

func handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	state.mu.Lock()
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if path == "" || dest == "" || !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dest)) {
		http.Error(w, "Forbidden", 403)
		return
	}
	runCommandAsync("DELETE SNAP", "🗑️", path, "btrfs", "subvolume", "delete", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}

func handleActionSnapshot(w http.ResponseWriter, r *http.Request) {
	go performSnapshot()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}

func performSnapshot() {
	state.mu.Lock()
	src := state.Config.SnapshotSource
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if src == "" || dest == "" {
		return
	}
	os.MkdirAll(dest, 0755)

	now := time.Now()
	name := now.Format(timeLayout)
	fullDest := fmt.Sprintf("%s/%s", strings.TrimRight(dest, "/"), name)
	visualPath := fmt.Sprintf("%s ➡️ %s", src, name)

	printDockerLog("SNAPSHOT", "Creating snapshot %s -> %s", src, fullDest)

	cmd := exec.Command("btrfs", "subvolume", "snapshot", "-r", src, fullDest)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	status := "Success"
	if err != nil {
		status = "Failed"
	}

	logHistory("SNAPSHOT", "📸", visualPath, status, outputStr)

	if status == "Success" {
		enforceRetention(dest)
	}
}

func enforceRetention(destPath string) {
	state.mu.Lock()
	cfg := state.Config.Retention
	state.mu.Unlock()

	if !cfg.Enabled {
		return
	}
	entries, err := os.ReadDir(destPath)
	if err != nil {
		return
	}

	type SnapInfo struct {
		Name string
		Time time.Time
	}
	var snaps []SnapInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := time.Parse(timeLayout, e.Name())
		if err == nil {
			snaps = append(snaps, SnapInfo{Name: e.Name(), Time: t})
		}
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })

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
		case "days":
			cutoff = now.AddDate(0, 0, -cfg.Value)
		case "weeks":
			cutoff = now.AddDate(0, 0, -cfg.Value*7)
		case "months":
			cutoff = now.AddDate(0, -cfg.Value, 0)
		case "years":
			cutoff = now.AddDate(-cfg.Value, 0, 0)
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

// --- Scheduler ---

func checkMissedSnapshots() {
	time.Sleep(2 * time.Second)
	state.mu.Lock()
	sched := state.Config.SnapshotSched
	dest := state.Config.SnapshotDest
	state.mu.Unlock()

	if !sched.Enabled || dest == "" || sched.Type != "every_x" {
		return
	}

	val, _ := strconv.Atoi(sched.Value)
	if val == 0 {
		val = 1
	}
	var duration time.Duration
	switch sched.Unit {
	case "minutes":
		duration = time.Duration(val) * time.Minute
	case "hours":
		duration = time.Duration(val) * time.Hour
	case "days":
		duration = time.Duration(val) * 24 * time.Hour
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		return
	}
	var lastTime time.Time
	found := false
	for _, e := range entries {
		if e.IsDir() {
			t, err := time.Parse(timeLayout, e.Name())
			if err == nil && t.After(lastTime) {
				lastTime = t
				found = true
			}
		}
	}

	if found {
		if time.Since(lastTime) > duration {
			printDockerLog("CATCHUP", "Missed schedule detected! Triggering now.")
			performSnapshot()
		}
	} else {
		printDockerLog("CATCHUP", "No snapshots found. Triggering initial.")
		performSnapshot()
	}
}

func refreshSchedules() {
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, id := range state.cronIDs {
		state.cron.Remove(id)
	}
	state.cronIDs = make(map[string]cron.EntryID)

	addJob := func(name string, cfg ScheduleConfig, job func()) {
		if !cfg.Enabled {
			return
		}
		spec := cfg.Value
		if cfg.Type == "every_x" {
			val, _ := strconv.Atoi(cfg.Value)
			unit := "m"
			if cfg.Unit == "days" {
				spec = fmt.Sprintf("@every %dh", val*24)
			} else {
				if cfg.Unit == "hours" {
					unit = "h"
				}
				spec = fmt.Sprintf("@every %d%s", val, unit)
			}
		}
		id, err := state.cron.AddFunc(spec, job)
		if err == nil {
			printDockerLog("SCHEDULER", "Registered %s: %s", name, spec)
			state.cronIDs[name] = id
		}
	}

	addJob("snapshot", state.Config.SnapshotSched, func() { go performSnapshot() })
	addJob("scrub", state.Config.ScrubSched, func() {
		p := state.Config.TargetDrive
		if p != "" {
			runCommandAsync("AUTO SCRUB", "🧹", p, "btrfs", "scrub", "start", "-B", p)
		}
	})
	addJob("balance", state.Config.BalanceSched, func() {
		p := state.Config.TargetDrive
		if p != "" {
			runCommandAsync("AUTO BALANCE", "⚖️", p, "btrfs", "balance", "start", "--full-balance", p)
		}
	})
}

// --- Standard Handlers ---

func handleActionScrub(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	path := state.Config.TargetDrive
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}
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
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}
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
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}
	id := runCommandAsync("DEFRAG", "📦", path, "btrfs", "filesystem", "defragment", "-r", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handleActionCompsize(w http.ResponseWriter, r *http.Request) {
	path := state.Config.TargetDrive
	if path == "" {
		http.Error(w, "Target drive not set", 400)
		return
	}
	id := runCommandAsync("COMPSIZE", "📊", path, "compsize", path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func handlePurgeAllSnapshots(w http.ResponseWriter, r *http.Request) {
	go func() {
		state.mu.Lock()
		dest := state.Config.SnapshotDest
		state.mu.Unlock()
		if dest == "" {
			return
		}
		entries, _ := os.ReadDir(dest)
		count := 0
		for _, e := range entries {
			if e.IsDir() {
				if _, err := time.Parse(timeLayout, e.Name()); err == nil {
					exec.Command("btrfs", "subvolume", "delete", filepath.Join(dest, e.Name())).Run()
					count++
				}
			}
		}
		runCommandAsync("PURGE ALL", "🔥", dest, "echo", fmt.Sprintf("Deleted %d snapshots", count))
	}()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "triggered"})
}

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

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	state.History = []LogEntry{}
	state.mu.Unlock()
	saveState()
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "cleared"})
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
		// If map was nil from JSON (empty file), make it
		if state.Profiles == nil {
			state.Profiles = make(map[string]Config)
		}
	}
}
