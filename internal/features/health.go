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

	// -b forces byte output for easy parsing
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

	// Parse total used for Data and Metadata
	// Output format example: "Data,single: Size:8.00GiB, Used:6.54GiB (100.00%)" -> in bytes with -b: "Used:7025459200"
	// We sum up all "Used" occurrences for Data and Metadata types
	
	var dataUsed, metaUsed int64
	
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Match lines starting with Data or Metadata
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
	
	// Stats map: Device -> {ErrorType: Value}
	stats := make(map[string]map[string]string) 
	
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" { continue }
		
		// Format: [/dev/sda].write_io_errs 0
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			rawKey := parts[0] // [/dev/sda].write_io_errs
			valStr := parts[1] // 0
			
			// Extract Device Name
			// 1. Remove brackets [ ]
			clean := strings.ReplaceAll(rawKey, "[", "")
			clean = strings.ReplaceAll(clean, "]", "")
			
			// 2. Split by last dot to get device and error type
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
	path := core.State.Config.TargetDrive
	if path == "" { http.Error(w, "Target", 400); return }
	
	dfOut, _ := exec.Command("df", path).Output()
	lines := strings.Split(strings.TrimSpace(string(dfOut)), "\n")
	if len(lines) < 2 { http.Error(w, "Resolve failed", 500); return }
	
	// /dev/sda1 -> /dev/sda
	device := strings.Fields(lines[1])[0] 
	device = strings.TrimRight(device, "0123456789")

	cmd := exec.Command("smartctl", "-a", "-j", device)
	out, _ := cmd.CombinedOutput()
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}
