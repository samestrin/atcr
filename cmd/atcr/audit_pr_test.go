package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPRNumberFromFlags(t *testing.T) {
	tests := []struct {
		name      string
		flagSet   bool
		flagVal   int
		githubRef string
		want      int
	}{
		{name: "flag set wins", flagSet: true, flagVal: 1234, githubRef: "refs/pull/9/merge", want: 1234},
		{name: "github ref fallback (merge)", githubRef: "refs/pull/42/merge", want: 42},
		{name: "github ref fallback (head)", githubRef: "refs/pull/7/head", want: 7},
		{name: "neither flag nor env", want: 0},
		{name: "non-pr ref ignored", githubRef: "refs/heads/main", want: 0},
		{name: "malformed pull ref ignored", githubRef: "refs/pull/abc/merge", want: 0},
		{name: "flag zero falls back to env", flagSet: false, githubRef: "refs/pull/3/merge", want: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_REF", tc.githubRef)
			cmd := newReviewCmd()
			if tc.flagSet {
				require := assert.New(t)
				require.NoError(cmd.Flags().Set("pr", intToStr(tc.flagVal)))
			}
			got := prNumberFromFlags(cmd)
			assert.Equal(t, tc.want, got)
		})
	}
}

func intToStr(n int) string {
	// small local helper to avoid importing strconv just for the test table
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
