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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

var cronRunner *cron.Cron
var cronIDs map[string]cron.EntryID
var pendingRunTimes map[string]time.Time 
var pendingRunTimesMu sync.Mutex

func main() {
	core.LoadState()
	
	cronRunner = cron.New()
	cronIDs = make(map[string]cron.EntryID)
	pendingRunTimes = make(map[string]time.Time)
	
	cronRunner.Start()
	smartScheduleJobs()

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

// --- SMART SCHEDULER LOGIC ---

func smartScheduleJobs() {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	
	for _, id := range cronIDs { cronRunner.Remove(id) }
	cronIDs = make(map[string]cron.EntryID)
	
	pendingRunTimesMu.Lock()
	pendingRunTimes = make(map[string]time.Time)
	pendingRunTimesMu.Unlock()

	for _, job := range core.State.Config.Jobs {
		if !job.Schedule.Enabled { continue }

		val, _ := strconv.Atoi(job.Schedule.Value)
		if val <= 0 { val = 1 }
		var interval time.Duration
		var cronSpec string

		if job.Schedule.Type == "every_x" {
			switch job.Schedule.Unit {
			case "minutes": 
				interval = time.Duration(val) * time.Minute
				cronSpec = fmt.Sprintf("@every %dm", val)
			case "hours": 
				interval = time.Duration(val) * time.Hour
				cronSpec = fmt.Sprintf("@every %dh", val)
			case "days": 
				interval = time.Duration(val) * 24 * time.Hour
				cronSpec = fmt.Sprintf("@every %dh", val * 24)
			}
		} else {
			addRecurringJob(job, job.Schedule.Value)
			continue
		}

		lastSnapTime := getLastSnapshotTime(job.Dest)
		now := time.Now()
		
		core.PrintConsole("DEBUG", "Job '%s' (Interval: %s). Last Snap Detected: %s", job.Name, interval, lastSnapTime.Format(time.RFC3339))
		
		if lastSnapTime.IsZero() {
			core.PrintConsole("SCHEDULER", "Job %s: No valid history found. Running Initial.", job.Name)
			go runAndSchedule(job, cronSpec)
		} else {
			nextDue := lastSnapTime.Add(interval)
			timeUntil := nextDue.Sub(now)

			if timeUntil <= 0 {
				core.PrintConsole("SCHEDULER", "Job %s: Overdue by %s. Running Catch-up.", job.Name, (-timeUntil).String())
				go runAndSchedule(job, cronSpec)
			} else {
				core.PrintConsole("SCHEDULER", "Job %s: Resuming schedule. Next run in %s", job.Name, timeUntil.Round(time.Second))
				pendingRunTimesMu.Lock()
				pendingRunTimes[job.ID] = nextDue
				pendingRunTimesMu.Unlock()
				time.AfterFunc(timeUntil, func() { runAndSchedule(job, cronSpec) })
			}
		}
	}
}

func runAndSchedule(job config.BackupJob, spec string) {
	features.PerformBackupJob(job)
	pendingRunTimesMu.Lock()
	delete(pendingRunTimes, job.ID)
	pendingRunTimesMu.Unlock()
	addRecurringJob(job, spec)
}

func addRecurringJob(job config.BackupJob, spec string) {
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	if _, exists := cronIDs["job_"+job.ID]; exists { return }

	id, err := cronRunner.AddFunc(spec, func() { go features.PerformBackupJob(job) })
	if err == nil {
		cronIDs["job_"+job.ID] = id
		core.PrintConsole("SCHEDULER", "Job %s: Recurring loop active (%s)", job.Name, spec)
	} else {
		core.PrintConsole("ERROR", "Job %s: Cron failed %v", job.Name, err)
	}
}

func getLastSnapshotTime(dest string) time.Time {
	entries, err := os.ReadDir(dest)
	if err != nil { return time.Time{} }

	var newest time.Time
	for _, e := range entries {
		if e.IsDir() {
			t := features.ParseSnapshotTime(e)
			if t.After(newest) { newest = t }
		}
	}
	return newest
}

// --- Handlers ---
func handleJobStatus(w http.ResponseWriter, r *http.Request) {
	core.State.Mu.Lock()
	jobs := core.State.Config.Jobs
	core.State.Mu.Unlock()
	
	status := make(map[string]interface{})
	pendingRunTimesMu.Lock()
	defer pendingRunTimesMu.Unlock()

	for _, job := range jobs {
		if t, ok := pendingRunTimes[job.ID]; ok {
			status[job.ID] = t
			continue
		}
		key := "job_" + job.ID
		if eid, exists := cronIDs[key]; exists {
			entry := cronRunner.Entry(eid)
			status[job.ID] = entry.Next
		} else {
			status[job.ID] = nil
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
			go smartScheduleJobs()
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

// Dummy stub if needed
func refreshSchedules() {
	smartScheduleJobs()
}
