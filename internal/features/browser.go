package features

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

func HandleBrowserList(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] HandleBrowserList called for path: %s", reqPath)

	if reqPath == "" {
		core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Error: Path is empty")
		http.Error(w, "Path required", 400)
		return
	}

	// SECURITY: Ensure we are browsing inside one of our Destination folders
	allowed := false
	core.State.Mu.Lock()
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Checking %d jobs for path authorization", len(core.State.Config.Jobs))
	for _, job := range core.State.Config.Jobs {
		// Resolve symlinks and reject traversal to prevent escaping the
		// backup tree or following symlinks out of it.
		if pathWithin(job.Dest, reqPath) {
			allowed = true
			core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Path authorized by job: %s (dest: %s)", job.Name, job.Dest)
			break
		}
	}
	core.State.Mu.Unlock()

	if !allowed {
		core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Access denied: path %s not in backup destinations", reqPath)
		http.Error(w, "Access Denied: Path not in backup destinations", 403)
		return
	}

	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Reading directory: %s", reqPath)
	entries, err := os.ReadDir(reqPath)
	if err != nil {
		core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Error reading directory: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Found %d entries", len(entries))

	type FileItem struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
		Path  string `json:"path"`
	}
	var files []FileItem
	for _, e := range entries {
		info, _ := e.Info()
		files = append(files, FileItem{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			Path:  filepath.Join(reqPath, e.Name()),
		})
	}
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Processed %d files", len(files))

	// Sort folders first
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Sorting files (directories first)")
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	json.NewEncoder(w).Encode(files)
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Returned %d files", len(files))
}

func HandleBrowserDownload(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] HandleBrowserDownload called for path: %s", reqPath)

	if reqPath == "" {
		core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Error: Path is empty")
		http.Error(w, "Path required", 400)
		return
	}

	// SECURITY CHECK
	allowed := false
	core.State.Mu.Lock()
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Checking authorization for download")
	for _, job := range core.State.Config.Jobs {
		if pathWithin(job.Dest, reqPath) {
			allowed = true
			core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Download authorized by job: %s", job.Name)
			break
		}
	}
	core.State.Mu.Unlock()

	if !allowed {
		core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Access denied for download: %s", reqPath)
		http.Error(w, "Access Denied", 403)
		return
	}

	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] Serving file: %s", reqPath)
	http.ServeFile(w, r, reqPath)
	core.PrintConsole(config.LogLevelVerbose, "[BROWSER] File served successfully")
}
