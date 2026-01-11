package features

import (
	"btrfs-commander/internal/core"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func HandleBrowserList(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" { http.Error(w, "Path required", 400); return }

	// SECURITY: Ensure we are browsing inside one of our Destination folders
	allowed := false
	core.State.Mu.Lock()
	for _, job := range core.State.Config.Jobs {
		// Clean paths to prevent ../ traversal attacks
		if strings.HasPrefix(filepath.Clean(reqPath), filepath.Clean(job.Dest)) {
			allowed = true; break
		}
	}
	core.State.Mu.Unlock()

	if !allowed { http.Error(w, "Access Denied: Path not in backup destinations", 403); return }

	entries, err := os.ReadDir(reqPath)
	if err != nil { http.Error(w, err.Error(), 500); return }

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
	
	// Sort folders first
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir { return files[i].IsDir }
		return files[i].Name < files[j].Name
	})

	json.NewEncoder(w).Encode(files)
}

func HandleBrowserDownload(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" { http.Error(w, "Path required", 400); return }

	// SECURITY CHECK
	allowed := false
	core.State.Mu.Lock()
	for _, job := range core.State.Config.Jobs {
		if strings.HasPrefix(filepath.Clean(reqPath), filepath.Clean(job.Dest)) {
			allowed = true; break
		}
	}
	core.State.Mu.Unlock()

	if !allowed { http.Error(w, "Access Denied", 403); return }

	http.ServeFile(w, r, reqPath)
}
