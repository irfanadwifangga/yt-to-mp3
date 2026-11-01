package domain

import "testing"

func TestTrimValidation(t *testing.T) {
	ptr := func(v int64) *int64 { return &v }
	for _, tc := range []struct {
		name       string
		start, end *int64
		duration   int64
		valid      bool
	}{
		{"full", nil, nil, 0, true},
		{"valid", ptr(1250), ptr(3500), 5000, true},
		{"start only", ptr(0), nil, 5000, false},
		{"end only", nil, ptr(1000), 5000, false},
		{"negative", ptr(-1), ptr(1000), 5000, false},
		{"empty", ptr(1000), ptr(1000), 5000, false},
		{"past source", ptr(0), ptr(5001), 5000, false},
		{"unknown duration", ptr(0), ptr(1000), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if valid := ValidateTrim(tc.start, tc.end, tc.duration) == nil; valid != tc.valid {
				t.Fatalf("valid = %v, want %v", valid, tc.valid)
			}
		})
	}
}
