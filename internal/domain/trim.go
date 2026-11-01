package domain

// ValidateTrim uses integer milliseconds so the persisted range survives retries exactly.
func ValidateTrim(start, end *int64, duration int64) error {
	if start == nil && end == nil {
		return nil
	}
	if start == nil || end == nil || *start < 0 || *end <= *start || duration <= 0 || *end > duration {
		return NewError(CodeInvalidTrim, ClassPermanent, "rentang potongan tidak valid untuk durasi sumber")
	}
	return nil
}
