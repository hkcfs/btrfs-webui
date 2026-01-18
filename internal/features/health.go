package features

import (
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
	w.Header().Set("Content-Type", "application/json")
	path := core.State.Config.TargetDrive
	if path == "" { 
		json.NewEncoder(w).Encode(map[string]string{"error": "Target drive not set"})
		return 
	}

	out, _ := exec.Command("btrfs", "filesystem", "usage", "-b", path).Output()
	text := string(out)

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
	json.NewEncoder(w).Encode(resp)
}

func HandleBtrfsStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := core.State.Config.TargetDrive
	if path == "" { 
		json.NewEncoder(w).Encode(map[string]string{"error": "Target drive not set"})
		return 
	}
	
	out, _ := exec.Command("btrfs", "device", "stats", path).Output()
	stats := make(map[string]map[string]string) 
	
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" { continue }
		
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
				}
			}
		}
	}
	json.NewEncoder(w).Encode(stats)
}

func HandleSmartData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	device := resolveDevice(core.State.Config.TargetDrive)
	if device == "" { 
		json.NewEncoder(w).Encode(map[string]string{"error": "Could not resolve device"})
		return 
	}

	cmd := exec.Command("smartctl", "-a", "-j", device)
	out, _ := cmd.CombinedOutput()
	w.Write(out)
}

func HandleSmartTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	testType := r.URL.Query().Get("type")
	path := core.State.Config.TargetDrive
	
	device := resolveDevice(path)
	if device == "" {
		core.PrintConsole("ERROR", "SMART: Could not resolve device for path %s", path)
		json.NewEncoder(w).Encode(map[string]string{"error": "Could not resolve device path"})
		return
	}

	// This shows in Docker logs
	core.PrintConsole("DEFAULT", "Starting SMART %s test on %s", testType, device)
	
	// This starts the UI log
	id := core.RunCommandAsync("SMART TEST", "🩺", device, "smartctl", "-t", testType, device)
	
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

// Helper to find /dev/sda from /host/mnt/data
func resolveDevice(path string) string {
	if path == "" { return "" }
	out, err := exec.Command("df", path).Output()
	if err != nil { return "" }
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 { return "" }
	
	fullDev := strings.Fields(lines[1])[0]
	baseDev := strings.TrimRight(fullDev, "0123456789")
	if baseDev == "/dev/" { return fullDev }
	return baseDev
}
