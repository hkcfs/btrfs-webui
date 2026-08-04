package features

import (
	"btrfs-commander/internal/config"
	"btrfs-commander/internal/core"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	sessionDuration  = 24 * time.Hour
	maxLoginAttempts = 8
	lockoutDuration  = 5 * time.Minute
)

// Server-side session state. The token is random and never serialized, so a
// cookie cannot be forged without logging in first.
var (
	authMu        sync.Mutex
	validToken    string
	tokenExpiry   time.Time
	loginAttempts = make(map[string]int)
	lastAttempt   = make(map[string]time.Time)
)

func newSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Practically unreachable; still make it unique.
		b = []byte(time.Now().UTC().String() + ":" + core.State.Config.LogLevel)
	}
	return hex.EncodeToString(b)
}

// isAuthed reports whether the request carries the current valid session
// cookie, compared in constant time.
func isAuthed(r *http.Request) bool {
	authMu.Lock()
	ok := validToken != "" && time.Now().Before(tokenExpiry)
	authMu.Unlock()
	if !ok {
		return false
	}
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(validToken)) == 1
}

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

		if isAuthed(r) {
			core.PrintConsole(config.LogLevelVerbose, "[AUTH] Valid session, allowing access to %s", path)
			next(w, r)
			return
		}

		core.PrintConsole(config.LogLevelVerbose, "[AUTH] No valid session for %s", path)

		// The login endpoint must be reachable without a session.
		if path == "/api/login" {
			next(w, r)
			return
		}

		// API Calls -> 401
		if strings.HasPrefix(path, "/api") {
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
				core.PrintConsole(config.LogLevelVerbose, "[AUTH] ERROR: Login page not found")
				http.Error(w, "Login page missing. Please check server logs.", 500)
				return
			}
		}

		// Everything else: reject instead of silently falling through.
		http.NotFound(w, r)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func allowLoginAttempt(ip string) bool {
	authMu.Lock()
	defer authMu.Unlock()
	if loginAttempts[ip] >= maxLoginAttempts {
		if time.Since(lastAttempt[ip]) > lockoutDuration {
			loginAttempts[ip] = 0
			return true
		}
		return false
	}
	return true
}

func failLoginAttempt(ip string) {
	authMu.Lock()
	defer authMu.Unlock()
	loginAttempts[ip]++
	lastAttempt[ip] = time.Now()
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	pass := r.FormValue("password")
	expected := os.Getenv("PASSWORD")

	ip := clientIP(r)
	if !allowLoginAttempt(ip) {
		http.Error(w, "Too many login attempts. Try again later.", 429)
		return
	}

	// Constant-time compare; also refuse empty passwords so a missing
	// PASSWORD env var cannot be "logged into" with an empty field.
	if expected != "" && subtle.ConstantTimeCompare([]byte(pass), []byte(expected)) == 1 {
		authMu.Lock()
		validToken = newSessionToken()
		tokenExpiry = time.Now().Add(sessionDuration)
		authMu.Unlock()

		loginAttempts[ip] = 0

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    validToken,
			Expires:  tokenExpiry,
			Path:     "/",
			HttpOnly: true,
			// Strict blocks cross-site requests from carrying the cookie,
			// which prevents CSRF on the state-changing endpoints.
			SameSite: http.SameSiteStrictMode,
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true}`))
		return
	}

	failLoginAttempt(ip)
	// Clear cookie
	http.SetCookie(w, &http.Cookie{Name: "session_token", MaxAge: -1, Path: "/"})
	http.Error(w, "Invalid Password", 401)
}
