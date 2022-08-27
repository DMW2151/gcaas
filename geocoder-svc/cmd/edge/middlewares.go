package main

import (
	// standard lin
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	// external
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// https://stackoverflow.com/a/50567022
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	buf        *bytes.Buffer
}

// NewLoggingResponseWriter
func NewLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK, &bytes.Buffer{}}
}

// WriteHeader
func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Write
func (lrw *loggingResponseWriter) Write(p []byte) (int, error) {
	defer func() {
		lrw.buf.Write(p)
	}()
	return lrw.ResponseWriter.Write(p)
}

// setDefaultResponseHeadersMiddleware -
func setDefaultResponseHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
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

// loggingMiddleware
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var startTime = time.Now() // log request time
		var errBody EdgeErrorResponse

		lrw := NewLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)

		reqLogger := log.WithFields(log.Fields{
			"duration":   -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
			"status":     lrw.statusCode,
			"request-id": r.Context().Value("gcaas-request-id"),
			"path":       r.URL.Path,
			"method":     r.Method,
		})

		if lrw.statusCode != http.StatusOK {
			_ = json.NewDecoder(lrw.buf).Decode(&errBody)
			reqLogger.WithFields(log.Fields{
				"err": errBody.Error,
			}).Error("request failed")
			return
		}
		reqLogger.Info("request ok")
	})
}
