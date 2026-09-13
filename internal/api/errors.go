package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Kode transport. Kode bisnis dimiliki paket domain; yang di bawah ini
// khusus lapisan HTTP sehingga domain tetap tidak tahu soal HTTP.
const (
	CodeForbiddenHost    domain.ErrorCode = "FORBIDDEN_HOST"
	CodeForbiddenOrigin  domain.ErrorCode = "FORBIDDEN_ORIGIN"
	CodeUnauthorized     domain.ErrorCode = "UNAUTHORIZED"
	CodeUnsupportedMedia domain.ErrorCode = "UNSUPPORTED_MEDIA_TYPE"
	CodeBadRequest       domain.ErrorCode = "BAD_REQUEST"
	CodeNotFound         domain.ErrorCode = "NOT_FOUND"
)

// statusByCode memetakan kode domain ke status HTTP. Kode yang tidak
// terdaftar jatuh ke 500, yang merupakan default paling aman.
var statusByCode = map[domain.ErrorCode]int{
	domain.CodeInvalidURL:        http.StatusBadRequest,
	domain.CodeInvalidSetting:    http.StatusBadRequest,
	domain.CodeUnsupportedURL:    http.StatusBadRequest,
	domain.CodeLiveNotSupported:  http.StatusUnprocessableEntity,
	domain.CodeVideoUnavailable:  http.StatusNotFound,
	domain.CodeVideoPrivate:      http.StatusForbidden,
	domain.CodeGeoBlocked:        http.StatusForbidden,
	domain.CodeAgeRestricted:     http.StatusForbidden,
	domain.CodeRateLimited:       http.StatusTooManyRequests,
	domain.CodeToolMissing:       http.StatusServiceUnavailable,
	domain.CodeToolOutdated:      http.StatusServiceUnavailable,
	domain.CodeToolManifest:      http.StatusServiceUnavailable,
	domain.CodeChecksumMismatch:  http.StatusBadGateway,
	domain.CodeToolInstallFailed: http.StatusBadGateway,
	domain.CodeJobNotFound:       http.StatusNotFound,
	domain.CodeQueueFull:         http.StatusServiceUnavailable,
	domain.CodeDuplicateActive:   http.StatusConflict,
	domain.CodeTimeout:           http.StatusGatewayTimeout,
	domain.CodeDiskFull:          http.StatusInsufficientStorage,
	domain.CodeDialogUnavailable: http.StatusServiceUnavailable,
	domain.CodeToolUpdateCheck:   http.StatusServiceUnavailable,
}

// errorBody adalah amplop error yang dipakai seluruh endpoint.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    domain.ErrorCode `json:"code"`
	Message string           `json:"message"`
	Details map[string]any   `json:"details"`
}

// writeError mengirim error dalam bentuk kontrak standar.
//
// Field message hanya fallback untuk developer; klien menerjemahkan sendiri
// dari code. Detail mentah dari tool tidak pernah ikut ke sini.
func writeError(w http.ResponseWriter, status int, code domain.ErrorCode, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code:    code,
		Message: message,
		Details: map[string]any{},
	}})
}

// writeDomainError menerjemahkan error domain jadi respons HTTP.
func (s *Server) writeDomainError(w http.ResponseWriter, err error) {
	var derr *domain.Error
	if !errors.As(err, &derr) {
		s.log.Error("error tanpa kode domain", "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Terjadi kesalahan internal.")
		return
	}

	status, ok := statusByCode[derr.Code]
	if !ok {
		status = http.StatusInternalServerError
	}

	// Detail berupa kalimat hanya masuk log, tidak pernah ke UI. Details
	// yang terstruktur boleh ikut, karena isinya konteks yang dibutuhkan
	// klien untuk merangkai pesannya sendiri, misalnya kunci setelan yang
	// ditolak.
	s.log.Warn("permintaan gagal", "code", derr.Code, "detail", derr.Detail, "cause", derr.Cause)

	details := map[string]any{}
	for k, v := range derr.Details {
		details[k] = v
	}
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code:    derr.Code,
		Message: string(derr.Code),
		Details: details,
	}})
}

// writeJSON mengirim payload JSON dengan header yang benar.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Header sudah terkirim bila encoding gagal; tidak ada yang bisa
	// diperbaiki selain mencatatnya di level pemanggil.
	_ = json.NewEncoder(w).Encode(payload)
}
