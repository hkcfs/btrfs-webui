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
	"time"

	"github.com/robfig/cron/v3"
)

var cronRunner *cron.Cron
var cronIDs map[string]cron.EntryID

func main() {
	core.LoadState()
	
	cronRunner = cron.New()
	cronIDs = make(map[string]cron.EntryID)
	cronRunner.Start()
	refreshSchedules()

	go checkMissedSnapshots()

	mux := http.NewServeMux()

	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "static/index.html")
	})

	mux.HandleFunc("/api/login", features.HandleLogin)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/history", handleHistory)
	mux.HandleFunc("/api/logs/clear", handleClearLogs)
	
	// NEW: Endpoint to get next run times
	mux.HandleFunc("/api/jobs/status", handleJobStatus)
	
	mux.HandleFunc("/api/storage/usage", features.HandleStorageUsage)
	mux.HandleFunc("/api/health/smart", features.HandleSmartData)
	mux.HandleFunc("/api/health/test", features.HandleSmartTest)
	mux.HandleFunc("/api/health/btrfs", features.HandleBtrfsStats)
	mux.HandleFunc("/api/browser/list", features.HandleBrowserList)
	mux.HandleFunc("/api/browser/download", features.HandleBrowserDownload)
	
	mux.HandleFunc("/api/snapshots/list", features.HandleListSnapshots)
	mux.HandleFunc("/api/snapshots/stats", features.HandleJobStats)
	mux.HandleFunc("/api/snapshots/delete", features.HandleDeleteSnapshot)
	mux.HandleFunc("/api/snapshots/diff", features.HandleSnapshotDiff)
	mux.HandleFunc("/api/snapshots/rollback", features.HandleRollback)
	
	mux.HandleFunc("/api/action/", handleGenericAction)

	handler := features.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }
	fmt.Printf("🚀 BTRFS Commander started on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

// --- Scheduler Logic ---

func checkMissedSnapshots() {
	time.Sleep(3 * time.Second)
	core.State.Mu.Lock()
	jobs := core.State.Config.Jobs
	core.State.Mu.Unlock()

	for _, job := range jobs {
		if !job.Schedule.Enabled || job.Schedule.Type != "every_x" { continue }

		val, _ := strconv.Atoi(job.Schedule.Value)
		if val == 0 { val = 1 }
		var duration time.Duration
		switch job.Schedule.Unit {
		case "minutes": duration = time.Duration(val) * time.Minute
		case "hours": duration = time.Duration(val) * time.Hour
		case "days": duration = time.Duration(val) * 24 * time.Hour
		}

		entries, err := os.ReadDir(job.Dest)
		if err != nil { continue }

		var lastTime time.Time
		found := false
		for _, e := range entries {
			if e.IsDir() {
				t := features.ParseSnapshotTime(e)
				if t.After(lastTime) {
					lastTime = t
					found = true
				}
			}
		}

		if !found {
			core.PrintConsole("CATCHUP", "Job '%s': No snapshots found. Triggering initial backup.", job.Name)
			features.PerformBackupJob(job)
		} else {
			if time.Since(lastTime) > duration {
				core.PrintConsole("CATCHUP", "Job '%s' missed schedule. Triggering now.", job.Name)
				features.PerformBackupJob(job)
			}
		}
	}
}

func refreshSchedules() {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	for _, id := range cronIDs { cronRunner.Remove(id) }
	cronIDs = make(map[string]cron.EntryID)

	schedule := func(name, spec string, job func()) {
		id, err := cronRunner.AddFunc(spec, job)
		if err == nil { 
			cronIDs[name] = id
			core.PrintConsole("DEFAULT", "Scheduled %s: %s", name, spec) 
		} else {
			core.PrintConsole("ERROR", "Failed to schedule %s (%s): %v", name, spec, err)
		}
	}

	for _, job := range core.State.Config.Jobs {
		if job.Schedule.Enabled {
			spec := job.Schedule.Value 
			if job.Schedule.Type == "every_x" { 
				val, _ := strconv.Atoi(job.Schedule.Value)
				if val <= 0 { val = 1 }
				if job.Schedule.Unit == "days" { 
					hours := val * 24
					spec = fmt.Sprintf("@every %dh", hours)
				} else {
					unit := "m"
					if job.Schedule.Unit == "hours" { unit = "h" }
					spec = fmt.Sprintf("@every %d%s", val, unit)
				}
			}
			j := job
			schedule("job_"+j.ID, spec, func() { go features.PerformBackupJob(j) })
		}
	}
}

// --- Handlers ---

// Returns Next Run time for all jobs
func handleJobStatus(w http.ResponseWriter, r *http.Request) {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	
	status := make(map[string]interface{})
	
	for _, job := range core.State.Config.Jobs {
		key := "job_" + job.ID
		if eid, exists := cronIDs[key]; exists {
			entry := cronRunner.Entry(eid)
			status[job.ID] = entry.Next // Returns standard ISO time
		} else {
			status[job.ID] = nil // Disabled or not scheduled
		}
	}
	
	json.NewEncoder(w).Encode(status)
}

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
	queryAction := r.URL.Query().Get("action")
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
		go func() {
			core.State.Mu.Lock()
			jobs := core.State.Config.Jobs
			core.State.Mu.Unlock()
			for _, job := range jobs {
				features.EnforceRetention(job.Dest, config.RetentionConfig{Enabled: true, Mode: "count", Value: 0})
			}
			core.RunCommandAsync("PURGE", "🔥", "ALL", "echo", "Purge triggered")
		}()
		id = 1
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}
