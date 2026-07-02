package health

import "net/http"

// CheckMiddleware wraps an http.Handler and intercepts requests to the
// given path, returning a 200 OK with a JSON status body. All other requests
// are forwarded to the next handler.
func CheckMiddleware(path string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
