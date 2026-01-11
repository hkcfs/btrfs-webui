package features

import (
	"net/http"
	"os"
	"time"
)

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pass := os.Getenv("PASSWORD")
		if pass == "" {
			next(w, r)
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value != "valid_session" {
			// API Calls -> 401
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" && r.URL.Path != "/api/login" {
				http.Error(w, "Unauthorized", 401)
				return
			}
			// Root or Index -> Serve Login
			if r.URL.Path == "/" || r.URL.Path == "/index.html" {
				// Use ./static/login.html explicitly. 
				// In Docker WORKDIR is /root/ and static is copied to /root/static
				if _, err := os.Stat("static/login.html"); err == nil {
					http.ServeFile(w, r, "static/login.html")
				} else {
					http.Error(w, "Login page missing in container. Check build.", 500)
				}
				return
			}
		}
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
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
	} else {
		http.Error(w, "Invalid Password", 401)
	}
}
