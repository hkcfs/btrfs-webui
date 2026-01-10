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

	// Regex for robust parsing of "Used:" in different sections
	parse := func(regexStr string) int64 {
		re := regexp.MustCompile(regexStr)
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			val, _ := strconv.ParseInt(matches[1], 10, 64)
			return val
		}
		return 0
	}

	// Overall section usually has the summary
	resp := map[string]int64{
		"device_size":        parse(`Device size:\s+(\d+)`),
		"device_allocated":   parse(`Device allocated:\s+(\d+)`),
		"device_unallocated": parse(`Device unallocated:\s+(\d+)`),
		// Summing up Data+Metadata used is safer than relying on summary sometimes
		"used":               parse(`Data,.*?: Used:(\d+)`) + parse(`Metadata,.*?: Used:(\d+)`), 
		"metadata_used":      parse(`Metadata,.*?: Used:(\d+)`),
		"free":               parse(`Free \(estimated\):\s+(\d+)`),
	}
	json.NewEncoder(w).Encode(resp)
}

func HandleBtrfsStats(w http.ResponseWriter, r *http.Request) {
	path := core.State.Config.TargetDrive
	if path == "" { http.Error(w, "Target drive not set", 400); return }
	
	out, _ := exec.Command("btrfs", "device", "stats", path).Output()
	
	// Format: [/dev/sda].write_io_errs 0
	stats := make(map[string]map[string]string) // String to handle "OK" formatting in JS or here? Let's do raw values.
	
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 2 {
			// Fix the [ bracket issue
			devClean := strings.ReplaceAll(parts[0], "[", "")
			devClean = strings.ReplaceAll(devClean, "].", "") // remove trailing ].
			
			key := parts[0]
			if idx := strings.LastIndex(parts[0], "."); idx != -1 {
				key = parts[0][idx+1:]
			}
			
			if _, ok := stats[devClean]; !ok { stats[devClean] = make(map[string]string) }
			
			valInt, _ := strconv.Atoi(parts[1])
			if valInt == 0 {
				stats[devClean][key] = "OK"
			} else {
				stats[devClean][key] = parts[1] // Keep number if error
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
	device := strings.TrimRight(strings.Fields(lines[1])[0], "0123456789")

	cmd := exec.Command("smartctl", "-a", "-j", device)
	out, _ := cmd.CombinedOutput()
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}
