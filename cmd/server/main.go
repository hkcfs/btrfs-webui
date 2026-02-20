package main

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"btrfs-commander/internal/features"
	"encoding/json"
	"fmt"
	"io"
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
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] ====== BTRFS Commander Starting ======")
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Loading state from disk...")
	core.LoadState()
	
	// Debug: Print loaded config
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] After LoadState - Jobs: %d, TargetDrive: '%s', LogLevel: '%s'", 
		len(core.State.Config.Jobs), core.State.Config.TargetDrive, core.State.Config.LogLevel)
	
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Loaded %d jobs and %d history entries", 
		len(core.State.Config.Jobs), len(core.State.History))
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Log level: %s", core.State.Config.LogLevel)
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Target drive: %s", core.State.Config.TargetDrive)
	
	cronRunner = cron.New()
	cronIDs = make(map[string]cron.EntryID)
	pendingRunTimes = make(map[string]time.Time)
	
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Starting cron scheduler...")
	cronRunner.Start()
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Cron scheduler started")
	
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Initializing smart scheduler...")
	smartScheduleJobs()
	
	fmt.Println(">>> SMART SCHEDULER COMPLETE - Setting up HTTP server...")
	os.Stdout.Sync()

	mux := http.NewServeMux()
	fmt.Println(">>> MUX CREATED")

	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Setting up static file server...")
	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Static file server configured")

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		core.PrintConsole(config.LogLevelVerbose, "[HTTP] Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "static/index.html")
	})

	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Registering API handlers...")
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
	mux.HandleFunc("/api/snapshots/lock", features.HandleToggleLock)
	
	mux.HandleFunc("/api/action/", handleGenericAction)
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] All API handlers registered")

	handler := features.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }
	
	fmt.Println(">>> BEFORE SERVER START - Port:", port)
	fmt.Println(">>> AUTH middleware enabled:", os.Getenv("PASSWORD") != "")
	
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] Server will listen on port: %s", port)
	
	if os.Getenv("PASSWORD") != "" {
		core.PrintConsole(config.LogLevelVerbose, "[MAIN] Password protection: ENABLED")
	} else {
		core.PrintConsole(config.LogLevelVerbose, "[MAIN] Password protection: DISABLED")
	}
	
	fmt.Printf("🚀 BTRFS Commander started on :%s\n", port)
	os.Stdout.Sync()
	
	core.PrintConsole(config.LogLevelVerbose, "[MAIN] ====== Server Starting ======")
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

// --- SMART SCHEDULER LOGIC ---

func smartScheduleJobs() {
	core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] ====== Scheduling Jobs ======")
	
	core.State.Mu.Lock()
	jobCount := len(core.State.Config.Jobs)
	core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] Processing %d configured jobs", jobCount)
	defer core.State.Mu.Unlock()
	
	core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] Clearing existing cron jobs...")
	for id, entryID := range cronIDs { 
		core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] Removing cron job: %s", id)
		cronRunner.Remove(entryID) 
	}
	cronIDs = make(map[string]cron.EntryID)
	
	pendingRunTimesMu.Lock()
	pendingRunTimes = make(map[string]time.Time)
	pendingRunTimesMu.Unlock()
	core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] Cleared all pending run times")

	scheduledCount := 0
	for i, job := range core.State.Config.Jobs {
		core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d/%d] Processing job: %s (ID: %s)", 
			i+1, jobCount, job.Name, job.ID)
		
		if !job.Schedule.Enabled { 
			core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Job scheduling is DISABLED, skipping", i+1)
			continue 
		}
		
		core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Schedule config: type=%s, value=%s, unit=%s",
			i+1, job.Schedule.Type, job.Schedule.Value, job.Schedule.Unit)

		val, _ := strconv.Atoi(job.Schedule.Value)
		if val <= 0 { val = 1 }
		var interval time.Duration
		var cronSpec string

		if job.Schedule.Type == "every_x" {
			switch job.Schedule.Unit {
			case "minutes": 
				interval = time.Duration(val) * time.Minute
				cronSpec = fmt.Sprintf("@every %dm", val)
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Interval mode: %d minutes (%s)", i+1, val, cronSpec)
			case "hours": 
				interval = time.Duration(val) * time.Hour
				cronSpec = fmt.Sprintf("@every %dh", val)
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Interval mode: %d hours (%s)", i+1, val, cronSpec)
			case "days": 
				interval = time.Duration(val) * 24 * time.Hour
				cronSpec = fmt.Sprintf("@every %dh", val * 24)
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Interval mode: %d days (%s)", i+1, val, cronSpec)
			}
		} else {
			core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Cron mode: expression=%s", i+1, job.Schedule.Value)
			addRecurringJob(job, job.Schedule.Value)
			scheduledCount++
			continue
		}

		core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Checking last snapshot time in: %s", i+1, job.Dest)
		lastSnapTime := getLastSnapshotTime(job.Dest)
		now := time.Now()
		
		core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Job '%s': interval=%s, lastSnap=%s, now=%s",
			i+1, job.Name, interval, lastSnapTime.Format(time.RFC3339), now.Format(time.RFC3339))
		
		if lastSnapTime.IsZero() {
			core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] No previous snapshots found for job", i+1)
			core.PrintConsole("SCHEDULER", "Job %s: No valid history found. Running Initial.", job.Name)
			go runAndSchedule(job, cronSpec)
			scheduledCount++
		} else {
			nextDue := lastSnapTime.Add(interval)
			timeUntil := nextDue.Sub(now)
			
			core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Calculated nextDue=%s, timeUntil=%s",
				i+1, nextDue.Format(time.RFC3339), timeUntil)

			if timeUntil <= 0 {
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Job is OVERDUE by %s", i+1, (-timeUntil).String())
				core.PrintConsole("SCHEDULER", "Job %s: Overdue by %s. Running Catch-up.", job.Name, (-timeUntil).String())
				
				// Log the missed snapshot
				core.State.Mu.Lock()
				core.State.History = append([]core.LogEntry{{
					ID:        time.Now().UnixNano(),
					Type:      "SNAPSHOT",
					Emoji:     "⚠️",
					Path:      job.Dest,
					Timestamp: lastSnapTime.Add(interval).Format("02-01-2006 15:04 MST"),
					Status:    "Missed",
					Output:    fmt.Sprintf("Snapshot was scheduled for %s but was missed (overdue by %s). Catching up now.", lastSnapTime.Add(interval).Format(time.RFC3339), (-timeUntil).String()),
					Duration:  "0s",
				}}, core.State.History...)
				core.State.Mu.Unlock()
				core.SaveState()
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Logged missed snapshot event", i+1)

				go runAndSchedule(job, cronSpec)
				scheduledCount++
			} else {
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Job scheduled for future: %s from now", i+1, timeUntil.Round(time.Second))
				core.PrintConsole("SCHEDULER", "Job %s: Resuming schedule. Next run in %s", job.Name, timeUntil.Round(time.Second))
				pendingRunTimesMu.Lock()
				pendingRunTimes[job.ID] = nextDue
				pendingRunTimesMu.Unlock()
				core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] [%d] Set pending run time for job", i+1)
				time.AfterFunc(timeUntil, func() { 
					core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] Timer expired for job %s, running now", job.Name)
					runAndSchedule(job, cronSpec) 
				})
				scheduledCount++
			}
		}
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[SCHEDULER] ====== Scheduling Complete: %d jobs scheduled ======", scheduledCount)
}

func runAndSchedule(job config.BackupJob, spec string) {
	fmt.Println(">>> runAndSchedule STARTED for job:", job.Name)
	features.PerformBackupJob(job)
	fmt.Println(">>> runAndSchedule BACKUP COMPLETE for job:", job.Name)
	pendingRunTimesMu.Lock()
	delete(pendingRunTimes, job.ID)
	pendingRunTimesMu.Unlock()
	addRecurringJob(job, spec)
	fmt.Println(">>> runAndSchedule COMPLETE for job:", job.Name)
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
	core.PrintConsole(config.LogLevelVerbose, "[API] handleJobStatus called")
	
	core.State.Mu.Lock()
	jobs := core.State.Config.Jobs
	core.State.Mu.Unlock()
	
	status := make(map[string]interface{})
	pendingRunTimesMu.Lock()
	defer pendingRunTimesMu.Unlock()

	core.PrintConsole(config.LogLevelVerbose, "[API] Building status for %d jobs", len(jobs))
	for _, job := range jobs {
		if t, ok := pendingRunTimes[job.ID]; ok {
			status[job.ID] = t
			core.PrintConsole(config.LogLevelVerbose, "[API] Job %s: pending at %s", job.ID, t.Format(time.RFC3339))
			continue
		}
		key := "job_" + job.ID
		if eid, exists := cronIDs[key]; exists {
			entry := cronRunner.Entry(eid)
			status[job.ID] = entry.Next
			core.PrintConsole(config.LogLevelVerbose, "[API] Job %s: next run at %s", job.ID, entry.Next.Format(time.RFC3339))
		} else {
			status[job.ID] = nil
			core.PrintConsole(config.LogLevelVerbose, "[API] Job %s: not scheduled", job.ID)
		}
	}
	
	json.NewEncoder(w).Encode(status)
	core.PrintConsole(config.LogLevelVerbose, "[API] handleJobStatus complete, returned %d statuses", len(status))
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	core.PrintConsole(config.LogLevelVerbose, "[API] handleConfig called: method=%s", r.Method)
	
	core.State.Mu.Lock()
	defer core.State.Mu.Unlock()
	
	if r.Method == "POST" {
		core.PrintConsole(config.LogLevelVerbose, "[API] Processing POST request - updating configuration")
		
		// Read body first to debug
		body, _ := io.ReadAll(r.Body)
		bodyLen := 500
		if len(body) < bodyLen {
			bodyLen = len(body)
		}
		core.PrintConsole(config.LogLevelVerbose, "[API] POST body (first %d chars): %s", bodyLen, string(body[:bodyLen]))
		
		var newConfig config.GlobalConfig
		if err := json.Unmarshal(body, &newConfig); err != nil {
			core.PrintConsole(config.LogLevelVerbose, "[API] ERROR: Failed to decode config: %v", err)
			http.Error(w, "Failed to decode config: " + err.Error(), 400)
			return
		}
		
		oldJobCount := len(core.State.Config.Jobs)
		newJobCount := len(newConfig.Jobs)
		oldLogLevel := core.State.Config.LogLevel
		newLogLevel := newConfig.LogLevel
		
		core.PrintConsole(config.LogLevelVerbose, "[API] Config changes: jobs=%d->%d, logLevel=%s->%s",
			oldJobCount, newJobCount, oldLogLevel, newLogLevel)
		
		// Debug each job
		for i, job := range newConfig.Jobs {
			core.PrintConsole(config.LogLevelVerbose, "[API] Job[%d]: id=%s, name=%s, source=%s, dest=%s",
				i, job.ID, job.Name, job.Source, job.Dest)
		}
		
		core.State.Config = newConfig
		core.PrintConsole(config.LogLevelVerbose, "[API] Saving new configuration to disk...")
		core.SaveState()
		core.PrintConsole(config.LogLevelVerbose, "[API] Configuration saved, re-scheduling jobs...")
		go smartScheduleJobs()
		core.PrintConsole(config.LogLevelVerbose, "[API] Job re-scheduling initiated")
	} else {
		core.PrintConsole(config.LogLevelVerbose, "[API] Returning current configuration: %d jobs", len(core.State.Config.Jobs))
		
		// Debug current jobs
		for i, job := range core.State.Config.Jobs {
			core.PrintConsole(config.LogLevelVerbose, "[API] Current Job[%d]: id=%s, name=%s, source=%s, dest=%s",
				i, job.ID, job.Name, job.Source, job.Dest)
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(core.State.Config)
	core.PrintConsole(config.LogLevelVerbose, "[API] handleConfig complete, returning %d jobs", len(core.State.Config.Jobs))
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

	core.PrintConsole(config.LogLevelVerbose, "[API] handleGenericAction called: type=%s, action=%s", actionType, queryAction)

	switch actionType {
	case "job":
		jobID := r.URL.Query().Get("id")
		core.PrintConsole(config.LogLevelVerbose, "[API] Manual job trigger requested for job_id=%s", jobID)
		
		core.State.Mu.Lock()
		var tgt config.BackupJob
		found := false
		for _, j := range core.State.Config.Jobs {
			if j.ID == jobID { tgt = j; found = true; break }
		}
		core.State.Mu.Unlock()
		
		if found {
			core.PrintConsole(config.LogLevelVerbose, "[API] Found job '%s', triggering execution", tgt.Name)
			go features.PerformBackupJob(tgt)
			json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})
			core.PrintConsole(config.LogLevelVerbose, "[API] Job execution triggered")
			return
		} else {
			core.PrintConsole(config.LogLevelVerbose, "[API] Job not found: %s", jobID)
		}
	case "scrub":
		path := core.State.Config.TargetDrive
		core.PrintConsole(config.LogLevelVerbose, "[API] Scrub action: %s on path: %s", queryAction, path)
		if queryAction == "status" {
			id = core.RunCommandAsync("SCRUB", "🩺", path, "btrfs", "scrub", "status", path)
		} else if queryAction == "cancel" {
			id = core.RunCommandAsync("SCRUB", "🛑", path, "btrfs", "scrub", "cancel", path)
		} else {
			id = core.RunCommandAsync("SCRUB", "🧹", path, "btrfs", "scrub", "start", "-B", path)
		}
		core.PrintConsole(config.LogLevelVerbose, "[API] Scrub command ID: %d", id)
	case "balance":
		path := core.State.Config.TargetDrive
		core.PrintConsole(config.LogLevelVerbose, "[API] Balance action: %s on path: %s", queryAction, path)
		if queryAction == "status" {
			id = core.RunCommandAsync("BALANCE", "⚖️", path, "btrfs", "balance", "status", path)
		} else if queryAction == "cancel" {
			id = core.RunCommandAsync("BALANCE", "🛑", path, "btrfs", "balance", "cancel", path)
		} else {
			id = core.RunCommandAsync("BALANCE", "⚖️", path, "btrfs", "balance", "start", "--full-balance", path)
		}
		core.PrintConsole(config.LogLevelVerbose, "[API] Balance command ID: %d", id)
	case "defrag":
		path := core.State.Config.TargetDrive
		core.PrintConsole(config.LogLevelVerbose, "[API] Defrag on path: %s", path)
		id = core.RunCommandAsync("DEFRAG", "📦", path, "btrfs", "filesystem", "defragment", "-r", path)
		core.PrintConsole(config.LogLevelVerbose, "[API] Defrag command ID: %d", id)
	case "compsize":
		path := core.State.Config.TargetDrive
		core.PrintConsole(config.LogLevelVerbose, "[API] Compsize on path: %s", path)
		id = core.RunCommandAsync("COMPSIZE", "📊", path, "compsize", path)
		core.PrintConsole(config.LogLevelVerbose, "[API] Compsize command ID: %d", id)
	case "purge_all":
		core.PrintConsole(config.LogLevelVerbose, "[API] Purge ALL snapshots requested")
		go func() {
			core.PrintConsole(config.LogLevelVerbose, "[PURGE] Starting purge of all snapshots")
			core.State.Mu.Lock()
			jobs := core.State.Config.Jobs
			core.State.Mu.Unlock()
			core.PrintConsole(config.LogLevelVerbose, "[PURGE] Purging snapshots for %d jobs", len(jobs))
			for i, job := range jobs {
				core.PrintConsole(config.LogLevelVerbose, "[PURGE] [%d/%d] Purging job: %s, dest: %s", i+1, len(jobs), job.Name, job.Dest)
				features.EnforceRetention(job.Dest, config.RetentionConfig{Enabled: true, Mode: "count", Value: 0})
			}
			core.RunCommandAsync("PURGE", "🔥", "ALL", "echo", "Purge triggered")
			core.PrintConsole(config.LogLevelVerbose, "[PURGE] Purge complete")
		}()
		id = 1
		core.PrintConsole(config.LogLevelVerbose, "[API] Purge initiated")
	default:
		core.PrintConsole(config.LogLevelVerbose, "[API] Unknown action type: %s", actionType)
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[API] handleGenericAction complete, returning id=%d", id)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

// Dummy stub if needed
func refreshSchedules() {
	smartScheduleJobs()
}
