package core

import (
	"btrfs-commander/internal/config"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

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

// appStateLoad is used for JSON unmarshaling - excludes Mu
type appStateLoad struct {
	Config      config.GlobalConfig `json:"config"`
	History     []LogEntry         `json:"history"`
	LockedSnaps map[string]bool   `json:"locked_snaps"`
}

// AppState is the main application state
type AppState struct {
	Config      config.GlobalConfig `json:"config"`
	History     []LogEntry         `json:"history"`
	LockedSnaps map[string]bool   `json:"locked_snaps"`
	Mu          sync.Mutex        // Not serialized
}

var State = AppState{
	Config: config.GlobalConfig{
		LogLevel: config.LogLevelDefault,
	},
	History:     []LogEntry{},
	LockedSnaps: make(map[string]bool),
}

func PrintConsole(level string, msg string, args ...interface{}) {
	currentLevel := State.Config.LogLevel
	if currentLevel == config.LogLevelNone { return }
	if level == config.LogLevelVerbose && currentLevel == config.LogLevelDefault { return }
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Printf("[%s] [%s] %s\n", timestamp, level, fmt.Sprintf(msg, args...))
}

func RunCommandAsync(opType, emoji, path, cmdName string, args ...string) int64 {
	State.Mu.Lock()
	startTime := time.Now()
	entryID := time.Now().UnixNano()
	cmdStr := fmt.Sprintf("%s %s", cmdName, strings.Join(args, " "))

	PrintConsole(config.LogLevelVerbose, "[RUN] Creating async task: type=%s, id=%d, path=%s", opType, entryID, path)
	PrintConsole(config.LogLevelVerbose, "[RUN] Command: %s", cmdStr)

	entry := LogEntry{
		ID:        entryID,
		Type:      opType,
		Emoji:     emoji,
		Path:      path,
		Timestamp: startTime.Format("02-01-2006 15:04 MST"),
		Status:    "Running...",
		Output:    fmt.Sprintf("Command: %s", cmdStr),
	}
	State.History = append([]LogEntry{entry}, State.History...)
	State.Mu.Unlock()

	PrintConsole(config.LogLevelVerbose, "[RUN] Task %d queued, starting execution goroutine", entryID)

	go func() {
		PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Starting: %s", entryID, cmdStr)
		PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Binary: %s, Args: %v", entryID, cmdName, args)
		
		cmd := exec.Command(cmdName, args...)
		PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Command object created, running...", entryID)
		
		output, err := cmd.CombinedOutput()
		duration := time.Since(startTime).Round(time.Millisecond)
		outputStr := string(output)
		
		outputLen := len(outputStr)
		if outputLen > 500 {
			PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Output received (truncated): %s... [%d chars total]", entryID, outputStr[:500], outputLen)
		} else {
			PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Output received: %s", entryID, outputStr)
		}

		status := "Success"
		if err != nil { 
			status = "Failed" 
			PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Error: %v", entryID, err)
		}

		PrintConsole(config.LogLevelVerbose, "[EXEC] Task %d - Status: %s, Duration: %s", entryID, status, duration)

		if status == "Failed" {
			PrintConsole("ERROR", "%s Failed: %v", opType, err)
		} else {
			PrintConsole(config.LogLevelVerbose, "DONE %s (%s)", opType, duration)
		}

		State.Mu.Lock()
		defer State.Mu.Unlock()
		
		PrintConsole(config.LogLevelVerbose, "[SAVE] Task %d - Saving results to history", entryID)
		
		for i, e := range State.History {
			if e.ID == entryID {
				State.History[i].Duration = duration.String()
				State.History[i].Output = fmt.Sprintf("$ %s\n\n%s", cmdStr, outputStr)
				
				if err != nil {
					if strings.Contains(outputStr, "Operation in progress") || strings.Contains(outputStr, "inprogress") {
						State.History[i].Status = "Warning"
						State.History[i].Output += "\n\n⚠️ NOTE: Operation already running."
					} else {
						State.History[i].Status = "Failed"
						State.History[i].Output += fmt.Sprintf("\nError: %v", err)
					}
				} else {
					State.History[i].Status = "Success"
				}
				PrintConsole(config.LogLevelVerbose, "[SAVE] Task %d - Updated history entry at index %d with status=%s", entryID, i, State.History[i].Status)
				break
			}
		}
		SaveState()
		PrintConsole(config.LogLevelVerbose, "[SAVE] Task %d - State saved to disk", entryID)
	}()
	return entryID
}

func SaveState() {
	data, _ := json.MarshalIndent(State, "", "  ")
	os.WriteFile("/data/state.json", data, 0644)
}

func LoadState() {
	data, err := os.ReadFile("/data/state.json")
	if err != nil {
		fmt.Printf("No existing state file, using defaults: %v\n", err)
		return
	}
	
	// Use separate struct for loading to avoid Mu unmarshal issues
	var loaded appStateLoad
	if err := json.Unmarshal(data, &loaded); err != nil {
		fmt.Printf("ERROR: Failed to parse state.json: %v\n", err)
		maxLen := 500
		if len(data) < maxLen {
			maxLen = len(data)
		}
		fmt.Printf("Raw data (first %d chars): %s\n", maxLen, string(data[:maxLen]))
		return
	}
	
	State.Config = loaded.Config
	State.History = loaded.History
	State.LockedSnaps = loaded.LockedSnaps
	if State.LockedSnaps == nil {
		State.LockedSnaps = make(map[string]bool)
	}

	// --- MIGRATION LOGIC (RESTORED) ---
		// If jobs list is empty BUT we have old snapshot settings, migrate them.
		if len(State.Config.Jobs) == 0 && State.Config.SnapshotSource != "" {
			fmt.Println("Converting legacy configuration to new Job format...")
			
			// Build retention from pointer or default
			ret := config.RetentionConfig{Enabled: false, Mode: "count", Unit: "days", Value: 5}
			if State.Config.Retention != nil { ret = *State.Config.Retention }

			// Build schedule from pointer or default
			sched := config.ScheduleConfig{Enabled: false, Unit: "minutes", Value: "15", Type: "every_x"}
			if State.Config.SnapshotSched != nil { sched = *State.Config.SnapshotSched }

			newJob := config.BackupJob{
				ID:        "default_migrated",
				Name:      "Default Backup",
				Source:    State.Config.SnapshotSource,
				Dest:      State.Config.SnapshotDest,
				Schedule:  sched,
				Retention: ret,
			}
			State.Config.Jobs = append(State.Config.Jobs, newJob)
			
			// Clear legacy fields to prevent re-migration
			State.Config.SnapshotSource = ""
			State.Config.SnapshotDest = ""
			
			SaveState()
		}
		
		// --- NIL SLICE SAFETY (RESTORED) ---
	if State.Config.Jobs == nil { State.Config.Jobs = []config.BackupJob{} }
	if State.History == nil { State.History = []LogEntry{} }
	if State.LockedSnaps == nil { State.LockedSnaps = make(map[string]bool) }
}
