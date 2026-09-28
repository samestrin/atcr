package doctor

import (
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
)

// A genuine 4xx on the declared call is the provider rejecting the declaration,
// which repeats on every run, so it is not_honored when the same prompt without
// the declaration succeeds. Only when the control call also fails is the cause
// unknown, and the verdict stays unverified.
func TestRun_ThinkingDeclaredCallRejected(t *testing.T) {
	rejected := &llmclient.HTTPStatusError{Status: 400, Snippet: "unrecognized argument enable_thinking"}
	cases := []struct {
		name       string
		declErr    error
		controlErr error
		wantStatus string
		wantCalls  int
		wantDetail []string
	}{
		{name: "400, control passes", declErr: rejected, wantStatus: ThinkingNotHonored, wantCalls: 2,
			wantDetail: []string{"HTTP 400", "unrecognized argument enable_thinking", "without the declaration"}},
		{name: "422, control passes", declErr: &llmclient.HTTPStatusError{Status: 422, Snippet: "bad field"}, wantStatus: ThinkingNotHonored, wantCalls: 2,
			wantDetail: []string{"HTTP 422", "bad field"}},
		{name: "400, control fails too", declErr: rejected, controlErr: &llmclient.HTTPStatusError{Status: 400, Snippet: "bad request"}, wantStatus: ThinkingUnverified, wantCalls: 2,
			wantDetail: []string{"HTTP 400"}},
		{name: "408 stays transient", declErr: &llmclient.HTTPStatusError{Status: 408, Snippet: "timeout"}, wantStatus: ThinkingUnverified, wantCalls: 1},
		{name: "429 stays transient", declErr: &llmclient.HTTPStatusError{Status: 429, Snippet: "slow down"}, wantStatus: ThinkingUnverified, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, fake, _ := runThinking(t, thinkingTarget(t, "off", "", "qwen"), llmclient.Completion{}, tc.declErr, silent, tc.controlErr)
			assert.Equal(t, tc.wantStatus, a.ThinkingStatus, "detail: %s", a.ThinkingDetail)
			assert.Len(t, fake.completeCalls(), tc.wantCalls)
			for _, want := range tc.wantDetail {
				assert.Contains(t, a.ThinkingDetail, want)
			}
		})
	}
}
