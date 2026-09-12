package api

import (
	"encoding/json"
	"net/http"
)

// ErrorCode adalah set tertutup yang menjadi bagian kontrak API. Klien
// menerjemahkan kode ini sendiri; field message hanya fallback untuk
// developer dan log. Lihat docs planning "Kode error dan lokalisasi".
type ErrorCode string

const (
	CodeInvalidURL         ErrorCode = "INVALID_URL"
	CodeUnsupportedURL     ErrorCode = "UNSUPPORTED_URL"
	CodeLiveNotSupported   ErrorCode = "LIVE_NOT_SUPPORTED"
	CodeVideoUnavailable   ErrorCode = "VIDEO_UNAVAILABLE"
	CodeVideoPrivate       ErrorCode = "VIDEO_PRIVATE"
	CodeGeoBlocked         ErrorCode = "GEO_BLOCKED"
	CodeAgeRestricted      ErrorCode = "AGE_RESTRICTED"
	CodeRateLimited        ErrorCode = "RATE_LIMITED"
	CodeToolMissing        ErrorCode = "TOOL_MISSING"
	CodeToolOutdated       ErrorCode = "TOOL_OUTDATED"
	CodeDownloadFailed     ErrorCode = "DOWNLOAD_FAILED"
	CodeTranscodeFailed    ErrorCode = "TRANSCODE_FAILED"
	CodeVerifyFailed       ErrorCode = "VERIFY_FAILED"
	CodeDiskFull           ErrorCode = "DISK_FULL"
	CodeOutputWriteFailed  ErrorCode = "OUTPUT_WRITE_FAILED"
	CodeJobNotFound        ErrorCode = "JOB_NOT_FOUND"
	CodeQueueFull          ErrorCode = "QUEUE_FULL"
	CodeDuplicateActiveJob ErrorCode = "DUPLICATE_ACTIVE_JOB"
	CodeInterrupted        ErrorCode = "INTERRUPTED"
	CodeCancelled          ErrorCode = "CANCELLED"
	CodeInternal           ErrorCode = "INTERNAL"

	// Kode transport, dipakai middleware.
	CodeForbiddenHost    ErrorCode = "FORBIDDEN_HOST"
	CodeForbiddenOrigin  ErrorCode = "FORBIDDEN_ORIGIN"
	CodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	CodeUnsupportedMedia ErrorCode = "UNSUPPORTED_MEDIA_TYPE"
	CodeNotFound         ErrorCode = "NOT_FOUND"
)

// errorBody adalah amplop error yang dipakai seluruh endpoint.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// writeError mengirim error dalam bentuk kontrak standar.
func writeError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code:    code,
		Message: message,
		Details: map[string]any{},
	}})
}

// writeJSON mengirim payload JSON dengan header yang benar.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// Header sudah terkirim; tidak ada yang bisa diperbaiki di sini.
		return
	}
}
