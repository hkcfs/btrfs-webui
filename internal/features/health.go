package features

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"bufio"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func HandleStorageUsage(w http.ResponseWriter, r *http.Request) {
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleStorageUsage called")
	w.Header().Set("Content-Type", "application/json")
	
	path := core.State.Config.TargetDrive
	core.PrintConsole(config.LogLevelVerbose, "[STORAGE] Target drive: %s", path)
	
	if path == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[STORAGE] Error: Target drive not set")
		json.NewEncoder(w).Encode(map[string]string{"error": "Target drive not set"})
		return 
	}

	core.PrintConsole(config.LogLevelVerbose, "[STORAGE] Getting BTRFS filesystem usage for: %s", path)
	out, _ := exec.Command("btrfs", "filesystem", "usage", "-b", path).Output()
	text := string(out)
	core.PrintConsole(config.LogLevelVerbose, "[STORAGE] Got %d bytes of output", len(text))

	parse := func(regexStr string) int64 {
		re := regexp.MustCompile(regexStr)
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			val, _ := strconv.ParseInt(matches[1], 10, 64)
			return val
		}
		return 0
	}

	var dataUsed, metaUsed int64
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Data") {
			re := regexp.MustCompile(`Used:(\d+)`)
			if m := re.FindStringSubmatch(line); len(m) > 1 {
				v, _ := strconv.ParseInt(m[1], 10, 64)
				dataUsed += v
			}
		}
		if strings.HasPrefix(line, "Metadata") || strings.HasPrefix(line, "System") {
			re := regexp.MustCompile(`Used:(\d+)`)
			if m := re.FindStringSubmatch(line); len(m) > 1 {
				v, _ := strconv.ParseInt(m[1], 10, 64)
				metaUsed += v
			}
		}
	}

	resp := map[string]int64{
		"device_size":        parse(`Device size:\s+(\d+)`),
		"device_allocated":   parse(`Device allocated:\s+(\d+)`),
		"device_unallocated": parse(`Device unallocated:\s+(\d+)`),
		"used":               dataUsed + metaUsed,
		"metadata_used":      metaUsed,
		"free":               parse(`Free \(estimated\):\s+(\d+)`),
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[STORAGE] Results: size=%d, used=%d, free=%d", 
		resp["device_size"], resp["used"], resp["free"])
	json.NewEncoder(w).Encode(resp)
}

func HandleBtrfsStats(w http.ResponseWriter, r *http.Request) {
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleBtrfsStats called")
	w.Header().Set("Content-Type", "application/json")
	
	path := core.State.Config.TargetDrive
	core.PrintConsole(config.LogLevelVerbose, "[BTRFS-STATS] Target drive: %s", path)
	
	if path == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[BTRFS-STATS] Error: Target drive not set")
		json.NewEncoder(w).Encode(map[string]string{"error": "Target drive not set"})
		return 
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[BTRFS-STATS] Getting device stats for: %s", path)
	out, _ := exec.Command("btrfs", "device", "stats", path).Output()
	core.PrintConsole(config.LogLevelVerbose, "[BTRFS-STATS] Got %d bytes of output", len(out))
	
	stats := make(map[string]map[string]string) 
	
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	lineCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" { continue }
		lineCount++
		
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			rawKey := parts[0]
			valStr := parts[1]
			
			clean := strings.ReplaceAll(rawKey, "[", "")
			clean = strings.ReplaceAll(clean, "]", "")
			lastDot := strings.LastIndex(clean, ".")
			if lastDot != -1 {
				dev := clean[:lastDot]
				errType := clean[lastDot+1:]
				
				if _, ok := stats[dev]; !ok { stats[dev] = make(map[string]string) }
				
				if valStr == "0" {
					stats[dev][errType] = "OK"
				} else {
					stats[dev][errType] = valStr
					core.PrintConsole(config.LogLevelVerbose, "[BTRFS-STATS] Device %s has %s errors: %s", dev, errType, valStr)
				}
			}
		}
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[BTRFS-STATS] Parsed %d lines, found %d devices", lineCount, len(stats))
	json.NewEncoder(w).Encode(stats)
}

func HandleSmartData(w http.ResponseWriter, r *http.Request) {
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleSmartData called")
	w.Header().Set("Content-Type", "application/json")
	
	path := core.State.Config.TargetDrive
	core.PrintConsole(config.LogLevelVerbose, "[SMART] Resolving device for path: %s", path)
	
	device := resolveDevice(path)
	if device == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[SMART] Error: Could not resolve device for path %s", path)
		json.NewEncoder(w).Encode(map[string]string{"error": "Could not resolve device"})
		return 
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[SMART] Resolved device: %s", device)
	core.PrintConsole(config.LogLevelVerbose, "[SMART] Running: smartctl -a -j %s", device)
	
	cmd := exec.Command("smartctl", "-a", "-j", device)
	out, _ := cmd.CombinedOutput()
	
	core.PrintConsole(config.LogLevelVerbose, "[SMART] Got %d bytes of SMART data", len(out))
	w.Write(out)
}

func HandleSmartTest(w http.ResponseWriter, r *http.Request) {
	core.PrintConsole(config.LogLevelVerbose, "[API] HandleSmartTest called")
	w.Header().Set("Content-Type", "application/json")
	
	testType := r.URL.Query().Get("type")
	path := core.State.Config.TargetDrive
	
	core.PrintConsole(config.LogLevelVerbose, "[SMART-TEST] Test type: %s, path: %s", testType, path)
	
	device := resolveDevice(path)
	if device == "" {
		core.PrintConsole("ERROR", "SMART: Could not resolve device for path %s", path)
		core.PrintConsole(config.LogLevelVerbose, "[SMART-TEST] Error: Could not resolve device for path %s", path)
		json.NewEncoder(w).Encode(map[string]string{"error": "Could not resolve device path"})
		return
	}

	core.PrintConsole(config.LogLevelVerbose, "[SMART-TEST] Resolved device: %s", device)
	
	// This shows in Docker logs
	core.PrintConsole("DEFAULT", "Starting SMART %s test on %s", testType, device)
	
	// This starts the UI log
	core.PrintConsole(config.LogLevelVerbose, "[SMART-TEST] Starting async SMART test: smartctl -t %s %s", testType, device)
	id := core.RunCommandAsync("SMART TEST", "🩺", device, "smartctl", "-t", testType, device)
	
	core.PrintConsole(config.LogLevelVerbose, "[SMART-TEST] Test started with ID: %d", id)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

// Helper to find /dev/sda from /host/mnt/data
func resolveDevice(path string) string {
	core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Resolving device for path: %s", path)
	
	if path == "" { 
		core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Error: Path is empty")
		return "" 
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Running: df %s", path)
	out, err := exec.Command("df", path).Output()
	if err != nil { 
		core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Error running df: %v", err)
		return "" 
	}
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 { 
		core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Error: df output has insufficient lines")
		return "" 
	}
	
	fullDev := strings.Fields(lines[1])[0]
	baseDev := strings.TrimRight(fullDev, "0123456789")
	
	core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Full device: %s, Base device: %s", fullDev, baseDev)
	
	if baseDev == "/dev/" { 
		core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Returning full device: %s", fullDev)
		return fullDev 
	}
	
	core.PrintConsole(config.LogLevelVerbose, "[RESOLVE] Returning base device: %s", baseDev)
	return baseDev
}
