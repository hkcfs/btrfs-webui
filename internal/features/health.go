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
	path := core.State.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }

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
	path := core.State.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }
	
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
				if valStr == "0" { stats[dev][errType] = "OK" } else { stats[dev][errType] = valStr }
			}
		}
	}
	json.NewEncoder(w).Encode(stats)
}

func HandleSmartData(w http.ResponseWriter, r *http.Request) {
	device := resolveDevice(core.State.Config.TargetDrive)
	if device == "" { http.Error(w, "Could not resolve device", 500); return }

	cmd := exec.Command("smartctl", "-a", "-j", device)
	out, _ := cmd.CombinedOutput()
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func HandleSmartTest(w http.ResponseWriter, r *http.Request) {
	testType := r.URL.Query().Get("type")
	path := core.State.Config.TargetDrive
	
	device := resolveDevice(path)
	if device == "" {
		core.PrintConsole("ERROR", "SMART: Could not resolve device for path %s", path)
		http.Error(w, "Could not find device for path", 500)
		return
	}

	core.PrintConsole("SMART", "Starting %s test on %s", testType, device)
	id := core.RunCommandAsync("SMART TEST", "🩺", device, "smartctl", "-t", testType, device)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "id": id})
}

// Helper to find /dev/sda from /host/mnt/data
func resolveDevice(path string) string {
	if path == "" { return "" }
	// Try df to get the mounted device
	out, err := exec.Command("df", path).Output()
	if err != nil { return "" }
	
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 { return "" }
	
	// e.g. /dev/sda1
	fullDev := strings.Fields(lines[1])[0]
	
	// Strip numbers to get base device for smartctl (usually safer)
	// /dev/sda1 -> /dev/sda
	// /dev/nvme0n1p1 -> /dev/nvme0n1
	baseDev := strings.TrimRight(fullDev, "0123456789")
	
	// If the result is just "/dev/", something went wrong, stick to original
	if baseDev == "/dev/" { return fullDev }
	
	return baseDev
}
