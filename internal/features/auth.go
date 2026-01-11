package features

import (
	"net/http"
	"os"
	"time"
)

// Middleware to check cookie if PASSWORD env is set
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pass := os.Getenv("PASSWORD")
		if pass == "" {
			next(w, r) // No auth required
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value != "valid_session" {
			// If API call (except login), return 401
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" && r.URL.Path != "/api/login" {
				http.Error(w, "Unauthorized", 401)
				return
			}
			
			// If Root or Index, serve Login page
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				// Prevent caching of the login page so it doesn't get stuck
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")

				if _, err := os.Stat("static/login.html"); err == nil {
					http.ServeFile(w, r, "static/login.html")
				} else {
					http.Error(w, "Login page missing. Please rebuild container.", 500)
				}
				return
			}
		}
		
		// Cookie is valid, proceed to actual app
		next(w, r)
	}
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "Method not allowed", 405); return }
	pass := r.FormValue("password")
	expected := os.Getenv("PASSWORD")
	
	if pass == expected {
		// Create cookie
		cookie := &http.Cookie{
			Name:     "session_token",
			Value:    "valid_session",
			Expires:  time.Now().Add(24 * time.Hour),
			Path:     "/",
			HttpOnly: true,                // Security: JS cannot read it
			SameSite: http.SameSiteLaxMode, // Required for the redirect to work
		}
		http.SetCookie(w, cookie)
		
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
	} else {
		// Clear cookie just in case
		http.SetCookie(w, &http.Cookie{Name: "session_token", MaxAge: -1, Path: "/"})
		http.Error(w, "Invalid Password", 401)
	}
}
