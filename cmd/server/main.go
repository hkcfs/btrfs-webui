package main

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"btrfs-commander/internal/features"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/robfig/cron/v3"
)

var cronRunner *cron.Cron
var cronIDs map[string]cron.EntryID

func main() {
	core.LoadState()
	
	// Scheduler Init
	cronRunner = cron.New()
	cronIDs = make(map[string]cron.EntryID)
	cronRunner.Start()
	refreshSchedules()

	// --- Routes ---
	mux := http.NewServeMux()

	// Static Files
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	// Auth
	mux.HandleFunc("/api/login", features.HandleLogin)

	// API
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/history", handleHistory)
	mux.HandleFunc("/api/logs/clear", handleClearLogs)
	
	// Features
	mux.HandleFunc("/api/storage/usage", features.HandleStorageUsage)
	mux.HandleFunc("/api/health/smart", features.HandleSmartData)
	mux.HandleFunc("/api/health/btrfs", features.HandleBtrfsStats)
	// mux.HandleFunc("/api/health/test", features.HandleSmartTest) // TODO
	
	// Snapshots & Actions
	mux.HandleFunc("/api/snapshots/diff", features.HandleSnapshotDiff)
	mux.HandleFunc("/api/snapshots/rollback", features.HandleRollback)
	
	// Generic Action Handler wrapper
	mux.HandleFunc("/api/action/", handleGenericAction)

	// Apply Middleware
	handler := features.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }
	fmt.Printf("🚀 BTRFS Commander (Modular) started on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

// --- Local Handlers (Glue code) ---

func handleConfig(w http.ResponseWriter, r *http.Request) {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	if r.Method == "POST" {
		var newConfig config.GlobalConfig
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err == nil {
			core.State.Config = newConfig
			core.SaveState()
			go refreshSchedules()
		}
	}
	json.NewEncoder(w).Encode(core.State.Config)
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	json.NewEncoder(w).Encode(core.State.History)
}

func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	core.State.Mu.Lock()
	core.State.History = []core.LogEntry{}
	core.State.Mu.Unlock()
	core.SaveState()
	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

func handleGenericAction(w http.ResponseWriter, r *http.Request) {
	actionType := strings.TrimPrefix(r.URL.Path, "/api/action/")
	queryAction := r.URL.Query().Get("action") // start, stop, etc
	
	var id int64 = 0

	switch actionType {
	case "job":
		jobID := r.URL.Query().Get("id")
		core.State.Mu.Lock()
		var tgt config.BackupJob
		found := false
		for _, j := range core.State.Config.Jobs {
			if j.ID == jobID { tgt = j; found = true; break }
		}
		core.State.Mu.Unlock()
		if found {
			go features.PerformBackupJob(tgt)
			json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})
			return
		}
	case "scrub":
		path := core.State.Config.TargetDrive
		if queryAction == "status" {
			id = core.RunCommandAsync("SCRUB", "🩺", path, "btrfs", "scrub", "status", path)
		} else if queryAction == "cancel" {
			id = core.RunCommandAsync("SCRUB", "🛑", path, "btrfs", "scrub", "cancel", path)
		} else {
			id = core.RunCommandAsync("SCRUB", "🧹", path, "btrfs", "scrub", "start", "-B", path)
		}
	case "balance":
		path := core.State.Config.TargetDrive
		if queryAction == "status" {
			id = core.RunCommandAsync("BALANCE", "⚖️", path, "btrfs", "balance", "status", path)
		} else if queryAction == "cancel" {
			id = core.RunCommandAsync("BALANCE", "🛑", path, "btrfs", "balance", "cancel", path)
		} else {
			id = core.RunCommandAsync("BALANCE", "⚖️", path, "btrfs", "balance", "start", "--full-balance", path)
		}
	case "defrag":
		path := core.State.Config.TargetDrive
		id = core.RunCommandAsync("DEFRAG", "📦", path, "btrfs", "filesystem", "defragment", "-r", path)
	case "compsize":
		path := core.State.Config.TargetDrive
		id = core.RunCommandAsync("COMPSIZE", "📊", path, "compsize", path)
	case "purge_all":
		// Trigger purge for all jobs
		go func() {
			core.State.Mu.Lock()
			jobs := core.State.Config.Jobs
			core.State.Mu.Unlock()
			for _, job := range jobs {
				features.EnforceRetention(job.Dest, config.RetentionConfig{Enabled: true, Mode: "count", Value: 0}) // effectively delete all if 0, or custom logic needed for pure purge
			}
			core.RunCommandAsync("PURGE", "🔥", "ALL", "echo", "Purge triggered")
		}()
		id = 1
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

func refreshSchedules() {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	
	// Clear old
	for _, id := range cronIDs { cronRunner.Remove(id) }
	cronIDs = make(map[string]cron.EntryID)

	// Helper
	schedule := func(name, spec string, job func()) {
		id, err := cronRunner.AddFunc(spec, job)
		if err == nil { cronIDs[name] = id; core.PrintConsole("DEFAULT", "Scheduled %s: %s", name, spec) }
	}

	// Jobs
	for _, job := range core.State.Config.Jobs {
		if job.Schedule.Enabled {
			spec := job.Schedule.Value 
			// Simple parser for every_x
			if job.Schedule.Type == "every_x" { 
				unit := "m"
				if job.Schedule.Unit == "hours" { unit = "h" }
				if job.Schedule.Unit == "days" { unit = "d" } // robfig cron supports @every 1d
				spec = "@every " + job.Schedule.Value + unit
			}
			
			j := job
			schedule("job_"+j.ID, spec, func() { go features.PerformBackupJob(j) })
		}
	}
}
