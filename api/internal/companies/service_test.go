package companies

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDetectProvider tests the pure DetectProvider function.
func TestDetectProvider(t *testing.T) {
	cases := []struct {
		careersURL string
		want       string
	}{
		{"https://boards.greenhouse.io/acme", "greenhouse"},
		{"https://jobs.ashby.com/acme", "ashby"},
		{"https://acme.ashbyhq.com/jobs", "ashby"},
		{"https://jobs.lever.co/acme", "lever"},
		{"https://acme.recruitee.com", "recruitee"},
		{"https://careers.smartrecruiters.com/acme", "smartrecruiters"},
		{"https://acme.workable.com", "workable"},
		{"https://acme.com/careers", "unknown"},
		{"", "unknown"},
		{"not-a-url", "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.careersURL, func(t *testing.T) {
			got := DetectProvider(tc.careersURL)
			assert.Equal(t, tc.want, got)
		})
	}
}
