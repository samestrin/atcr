package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMeetsSeverityFloor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		minSev     string
		findingSev string
		want       bool
	}{
		{"default skips low", "MEDIUM", "LOW", false},
		{"default verifies medium", "MEDIUM", "MEDIUM", true},
		{"default verifies high", "MEDIUM", "HIGH", true},
		{"default verifies critical", "MEDIUM", "CRITICAL", true},
		{"high skips medium", "HIGH", "MEDIUM", false},
		{"high verifies high", "HIGH", "HIGH", true},
		{"low verifies everything", "LOW", "LOW", true},
		{"case-insensitive finding", "MEDIUM", "high", true},
		{"case-insensitive floor", "medium", "HIGH", true},
		{"empty finding severity skipped", "MEDIUM", "", false},
		{"unknown finding severity skipped", "MEDIUM", "BLOCKER", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, meetsSeverityFloor(tt.findingSev, tt.minSev))
		})
	}
}
