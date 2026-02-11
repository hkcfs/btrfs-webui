package features

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"net/http"
	"os"
	"time"
)

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pass := os.Getenv("PASSWORD")
		path := r.URL.Path
		
		core.PrintConsole(config.LogLevelVerbose, "[AUTH] Request: %s %s", r.Method, path)
		
		if pass == "" { 
			core.PrintConsole(config.LogLevelVerbose, "[AUTH] No password configured, allowing access")
			next(w, r) 
			return 
		}
		
		core.PrintConsole(config.LogLevelVerbose, "[AUTH] Password protection enabled")

		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value != "valid_session" {
			core.PrintConsole(config.LogLevelVerbose, "[AUTH] No valid session cookie found")
			
			// API Calls -> 401
			if len(path) >= 4 && path[:4] == "/api" && path != "/api/login" {
				core.PrintConsole(config.LogLevelVerbose, "[AUTH] API call without auth, returning 401")
				http.Error(w, "Unauthorized", 401)
				return
			}
			
			// Root or Index -> Serve Login
			if path == "/" || path == "/index.html" {
				core.PrintConsole(config.LogLevelVerbose, "[AUTH] Serving login page")
				// Prevent caching of the login page
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")

				// Try absolute path first (Docker), then relative (Local)
				if _, err := os.Stat("/root/static/login.html"); err == nil {
					core.PrintConsole(config.LogLevelVerbose, "[AUTH] Serving login from /root/static/login.html")
					http.ServeFile(w, r, "/root/static/login.html")
					return
				} else if _, err := os.Stat("static/login.html"); err == nil {
					core.PrintConsole(config.LogLevelVerbose, "[AUTH] Serving login from static/login.html")
					http.ServeFile(w, r, "static/login.html")
					return
				} else {
					// CRITICAL FIX: Do NOT call next(w,r). Stop here.
					// This prevents the infinite loop where it loads index.html instead.
					core.PrintConsole(config.LogLevelVerbose, "[AUTH] ERROR: Login page not found")
					http.Error(w, "Login page missing. Please check server logs.", 500)
					return
				}
			}
		}
		
		core.PrintConsole(config.LogLevelVerbose, "[AUTH] Valid session, allowing access to %s", path)
		next(w, r)
	}
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "Method not allowed", 405); return }
	pass := r.FormValue("password")
	expected := os.Getenv("PASSWORD")
	
	if pass == expected {
		http.SetCookie(w, &http.Cookie{
			Name: "session_token", Value: "valid_session", 
			Expires: time.Now().Add(24 * time.Hour), Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
	} else {
		// Clear cookie
		http.SetCookie(w, &http.Cookie{Name: "session_token", MaxAge: -1, Path: "/"})
		http.Error(w, "Invalid Password", 401)
	}
}
