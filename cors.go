package main

import "net/http"

// withCORS wraps an http.Handler to allow browser requests from the
// frontend's dev server (a different origin/port than this API during
// local development). Wide open (*) here on purpose — this is a local
// project, not a public deployment; a real deployment would restrict this
// to the frontend's actual origin instead of "*".
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
