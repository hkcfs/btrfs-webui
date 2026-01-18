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

type AppState struct {
	Config  config.GlobalConfig `json:"config"`
	History []LogEntry          `json:"history"`
	Mu      sync.Mutex
}

var State = AppState{
	Config: config.GlobalConfig{
		LogLevel: config.LogLevelDefault,
	},
	History: []LogEntry{},
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

	go func() {
		PrintConsole(config.LogLevelVerbose, "EXEC: %s", cmdStr)
		
		cmd := exec.Command(cmdName, args...)
		output, err := cmd.CombinedOutput()
		duration := time.Since(startTime).Round(time.Millisecond)
		outputStr := string(output)

		status := "Success"
		if err != nil { status = "Failed" }

		if status == "Failed" {
			PrintConsole("ERROR", "%s Failed: %v", opType, err)
		} else {
			PrintConsole(config.LogLevelVerbose, "DONE %s (%s)", opType, duration)
		}

		State.Mu.Lock()
		defer State.Mu.Unlock()
		
		for i, e := range State.History {
			if e.ID == entryID {
				State.History[i].Duration = duration.String()
				// Store Command + Output
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
				break
			}
		}
		SaveState()
	}()
	return entryID
}

func SaveState() {
	data, _ := json.MarshalIndent(State, "", "  ")
	os.WriteFile("/data/state.json", data, 0644)
}

func LoadState() {
	data, err := os.ReadFile("/data/state.json")
	if err == nil {
		var loaded AppState
		json.Unmarshal(data, &loaded)
		State.Config = loaded.Config
		State.History = loaded.History

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
	}
}
