package main

import (
	// standard lin
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	// external
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

// loggingMiddleware
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var startTime = time.Now() // log request time
		var errBody FailedRequest

		lrw := NewLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)

		reqLogger := log.WithFields(log.Fields{
			"duration(ms)":     -1 * float64(startTime.Sub(time.Now()).Microseconds()) / float64(1000),
			"status":           http.StatusText(lrw.statusCode),
			"status_code":      lrw.statusCode,
			"gcaas-request-id": r.Context().Value("gcaas-request-id"),
		})
		if lrw.statusCode != http.StatusOK {
			_ = json.NewDecoder(lrw.buf).Decode(&errBody)
			reqLogger.WithFields(log.Fields{
				"err": errBody.Error,
			}).Error("geocode request failed")
			return
		}
		reqLogger.Info("geocode request successful")
	})
}
