package core

import (
	"btrfs-commander/internal/config"
	"bytes"
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
	History     []LogEntry          `json:"history"`
	LockedSnaps map[string]bool     `json:"locked_snaps"`
}

// AppState is the main application state
type AppState struct {
	Config      config.GlobalConfig `json:"config"`
	History     []LogEntry          `json:"history"`
	LockedSnaps map[string]bool     `json:"locked_snaps"`
	Mu          sync.Mutex          // Not serialized
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
	if currentLevel == config.LogLevelNone {
		return
	}
	if level == config.LogLevelVerbose && currentLevel == config.LogLevelDefault {
		return
	}
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Printf("[%s] [%s] %s\n", timestamp, level, fmt.Sprintf(msg, args...))
}

// RunCommandAsync executes a single command without a shell and records the
// result in the history. All arguments are passed verbatim to the binary, so
// no shell metacharacter interpretation can occur.
func RunCommandAsync(opType, emoji, path, cmdName string, args ...string) int64 {
	cmdStr := strings.Join(append([]string{cmdName}, args...), " ")
	return runAsync(opType, emoji, path, cmdStr, func() ([]byte, error) {
		return exec.Command(cmdName, args...).CombinedOutput()
	})
}

// RunCommandPipelineAsync executes a shell-free pipeline: each []string is one
// command with its arguments, and the stdout of stage N feeds the stdin of
// stage N+1. No shell is involved, so user-supplied paths cannot inject
// commands.
func RunCommandPipelineAsync(opType, emoji, path string, cmds ...[]string) int64 {
	var parts []string
	for _, c := range cmds {
		parts = append(parts, strings.Join(c, " "))
	}
	cmdStr := strings.Join(parts, " | ")
	return runAsync(opType, emoji, path, cmdStr, func() ([]byte, error) {
		return runPipeline(cmds)
	})
}

// runPipeline runs cmds as a pipe chain and returns combined output plus the
// first error encountered.
func runPipeline(cmds [][]string) ([]byte, error) {
	if len(cmds) == 0 {
		return nil, nil
	}
	var stdout, stderr bytes.Buffer
	procs := make([]*exec.Cmd, len(cmds))
	var pipes []*os.File
	var prevRead *os.File
	for i, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stderr = &stderr
		if prevRead != nil {
			cmd.Stdin = prevRead
		}
		if i < len(cmds)-1 {
			r, w, err := os.Pipe()
			if err != nil {
				return nil, err
			}
			cmd.Stdout = w
			pipes = append(pipes, w, r)
			prevRead = r
		} else {
			cmd.Stdout = &stdout
		}
		procs[i] = cmd
	}

	for i := range procs {
		if err := procs[i].Start(); err != nil {
			return nil, err
		}
	}
	// Close the parent's copies so the children see EOF and pipes can drain.
	for _, f := range pipes {
		f.Close()
	}

	var firstErr error
	// Wait in reverse order to avoid deadlock on the pipes.
	for i := len(procs) - 1; i >= 0; i-- {
		if err := procs[i].Wait(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return append(stdout.Bytes(), stderr.Bytes()...), firstErr
}

func runAsync(opType, emoji, path, cmdStr string, run func() ([]byte, error)) int64 {
	State.Mu.Lock()
	startTime := time.Now()
	entryID := time.Now().UnixNano()

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

		output, err := run()
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
	// Marshal a copy that excludes the mutex (vet: copies lock value).
	stateCopy := struct {
		Config      config.GlobalConfig `json:"config"`
		History     []LogEntry          `json:"history"`
		LockedSnaps map[string]bool     `json:"locked_snaps"`
	}{State.Config, State.History, State.LockedSnaps}
	data, _ := json.MarshalIndent(stateCopy, "", "  ")
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
		if State.Config.Retention != nil {
			ret = *State.Config.Retention
		}

		// Build schedule from pointer or default
		sched := config.ScheduleConfig{Enabled: false, Unit: "minutes", Value: "15", Type: "every_x"}
		if State.Config.SnapshotSched != nil {
			sched = *State.Config.SnapshotSched
		}

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
	if State.Config.Jobs == nil {
		State.Config.Jobs = []config.BackupJob{}
	}
	if State.History == nil {
		State.History = []LogEntry{}
	}
	if State.LockedSnaps == nil {
		State.LockedSnaps = make(map[string]bool)
	}
}
