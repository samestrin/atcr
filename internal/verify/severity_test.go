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

func TestWithinComplexityCeiling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		estMinutes int
		maxMinutes int
		want       bool
	}{
		{"no ceiling passes large estimate", 100000, 0, true},
		{"no ceiling (negative) passes", 100000, -1, true},
		{"below ceiling passes", 10, 30, true},
		{"at ceiling passes (inclusive boundary)", 30, 30, true},
		{"above ceiling fails", 120, 30, false},
		{"one over ceiling fails", 31, 30, false},
		{"zero estimate is no-estimate, not skipped", 0, 30, true},
		{"negative estimate defensively not skipped", -5, 30, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, withinComplexityCeiling(tt.estMinutes, tt.maxMinutes))
		})
	}
}

func TestWithinSeverityCeiling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		findingSev string
		maxSev     string
		want       bool
	}{
		{"no ceiling passes critical", "CRITICAL", "", true},
		{"below ceiling passes", "LOW", "HIGH", true},
		{"at ceiling passes (inclusive)", "HIGH", "HIGH", true},
		{"above ceiling fails", "CRITICAL", "HIGH", false},
		{"medium ceiling skips high", "HIGH", "MEDIUM", false},
		{"case-insensitive finding", "critical", "HIGH", false},
		{"case-insensitive ceiling", "HIGH", "medium", false},
		{"unknown finding severity not skipped by ceiling", "BLOCKER", "HIGH", true},
		{"bogus non-empty ceiling fails closed", "LOW", "BOGUS", false},
		{"bogus ceiling skips critical", "CRITICAL", "BOGUS", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, withinSeverityCeiling(tt.findingSev, tt.maxSev))
		})
	}
}
