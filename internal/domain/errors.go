// Package domain memuat entity, state machine job, dan taksonomi error.
// Paket ini tidak boleh mengimpor apa pun dari layer lain.
package domain

import "fmt"

// ErrorCode adalah set tertutup yang menjadi bagian kontrak API. Klien
// menerjemahkan kode ini sendiri; pesan menyertainya hanya untuk developer
// dan log. Lihat docs planning "Kode error dan lokalisasi".
type ErrorCode string

const (
	CodeInvalidURL        ErrorCode = "INVALID_URL"
	CodeInvalidSetting    ErrorCode = "INVALID_SETTING"
	CodeUnsupportedURL    ErrorCode = "UNSUPPORTED_URL"
	CodeLiveNotSupported  ErrorCode = "LIVE_NOT_SUPPORTED"
	CodeVideoUnavailable  ErrorCode = "VIDEO_UNAVAILABLE"
	CodeVideoPrivate      ErrorCode = "VIDEO_PRIVATE"
	CodeGeoBlocked        ErrorCode = "GEO_BLOCKED"
	CodeAgeRestricted     ErrorCode = "AGE_RESTRICTED"
	CodeRateLimited       ErrorCode = "RATE_LIMITED"
	CodeToolMissing       ErrorCode = "TOOL_MISSING"
	CodeToolOutdated      ErrorCode = "TOOL_OUTDATED"
	CodeToolInstallFailed ErrorCode = "TOOL_INSTALL_FAILED"
	CodeToolManifest      ErrorCode = "TOOL_MANIFEST_INCOMPLETE"
	CodeChecksumMismatch  ErrorCode = "TOOL_CHECKSUM_MISMATCH"
	CodeDownloadFailed    ErrorCode = "DOWNLOAD_FAILED"
	CodeTranscodeFailed   ErrorCode = "TRANSCODE_FAILED"
	CodeVerifyFailed      ErrorCode = "VERIFY_FAILED"
	CodeDiskFull          ErrorCode = "DISK_FULL"
	CodeOutputWriteFailed ErrorCode = "OUTPUT_WRITE_FAILED"
	CodeDialogUnavailable ErrorCode = "DIALOG_UNAVAILABLE"
	CodeJobNotFound       ErrorCode = "JOB_NOT_FOUND"
	CodeQueueFull         ErrorCode = "QUEUE_FULL"
	CodeDuplicateActive   ErrorCode = "DUPLICATE_ACTIVE_JOB"
	CodeInterrupted       ErrorCode = "INTERRUPTED"
	CodeCancelled         ErrorCode = "CANCELLED"
	CodeTimeout           ErrorCode = "TIMEOUT"
	CodeInternal          ErrorCode = "INTERNAL"
)

// ErrorClass menentukan kebijakan retry. Lihat docs planning
// "Klasifikasi error dan kebijakan retry".
type ErrorClass string

const (
	ClassTransient    ErrorClass = "transient"
	ClassThrottled    ErrorClass = "throttled"
	ClassToolOutdated ErrorClass = "tool_outdated"
	ClassPermanent    ErrorClass = "permanent"
	ClassLocal        ErrorClass = "local"
)

// Error adalah error domain yang membawa kode kontrak dan kelas retry.
type Error struct {
	Code   ErrorCode
	Class  ErrorClass
	Detail string // untuk log dan job_events, tidak pernah untuk UI

	// Details membawa konteks terstruktur yang boleh sampai ke UI, misalnya
	// kunci setelan yang ditolak. Isinya tidak pernah berupa kalimat siap
	// tampil: klien tetap merangkai teksnya sendiri dari Code (ADR-027).
	Details map[string]string
	Cause   error
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

func (e *Error) Unwrap() error { return e.Cause }

// Retryable melaporkan apakah job boleh dicoba ulang otomatis.
func (e *Error) Retryable() bool {
	switch e.Class {
	case ClassTransient, ClassThrottled:
		return true
	default:
		return false
	}
}

// NewError membuat error domain.
func NewError(code ErrorCode, class ErrorClass, detail string) *Error {
	return &Error{Code: code, Class: class, Detail: detail}
}

// WrapError membungkus error asal dengan kode kontrak.
func WrapError(code ErrorCode, class ErrorClass, detail string, cause error) *Error {
	return &Error{Code: code, Class: class, Detail: detail, Cause: cause}
}
