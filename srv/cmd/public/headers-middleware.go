package main

import (
	// standard lib
	"context"
	"net/http"

	// external
	"github.com/google/uuid"
)

// setDefaultResponseHeadersMiddleware -
func setDefaultResponseHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// requestIDMiddleware -
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, "gcaas-request-id", uuid.New().String())
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}
