package langgraphcompat

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// AuthConfig holds server authentication settings.
type AuthConfig struct {
	// Token is the required Bearer token. If empty, auth is disabled.
	Token string
	// YOLOMode skips all auth checks when true.
	YOLOMode bool
}

// defaultAuthConfig returns auth config from environment.
func defaultAuthConfig() AuthConfig {
	return AuthConfig{
		Token:    strings.TrimSpace(os.Getenv("DEERFLOW_AUTH_TOKEN")),
		YOLOMode: strings.TrimSpace(os.Getenv("DEERFLOW_YOLO")) == "1" ||
			strings.EqualFold(strings.TrimSpace(os.Getenv("DEERFLOW_YOLO")), "true"),
	}
}

// wrapAuth returns an http.Handler that enforces Bearer token auth.
// Public paths (no auth required):
//   - GET /health
//   - GET /api/skills (read-only)
//   - OPTIONS (CORS preflight)
func wrapAuth(next http.Handler, cfg AuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.YOLOMode {
			next.ServeHTTP(w, r)
			return
		}

		// No token configured: skip auth (backward compatible for dev)
		if cfg.Token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Public paths (always allowed)
		path := r.URL.Path
		method := r.Method

		if path == "/health" ||
			(path == "/api/skills" && method == http.MethodGet) ||
			method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Check Bearer token
		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeJSONAuth(w, http.StatusUnauthorized, map[string]any{
				"detail": "missing Authorization header",
			})
			return
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeJSONAuth(w, http.StatusUnauthorized, map[string]any{
				"detail": "invalid Authorization format, expected: Bearer <token>",
			})
			return
		}

		given := parts[1]
		if subtle.ConstantTimeCompare([]byte(given), []byte(cfg.Token)) != 1 {
			writeJSONAuth(w, http.StatusUnauthorized, map[string]any{
				"detail": "invalid token",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSONAuth(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
