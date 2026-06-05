package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDetectPlatform tests the pure detectPlatform function.
// No mocks needed — pure hostname → platform mapping.
func TestDetectPlatform(t *testing.T) {
	cases := []struct {
		hostname string
		want     string
	}{
		{"boards.greenhouse.io", "greenhouse"},
		{"jobs.ashby.com", "ashby"},
		{"jobs.ashbyhq.com", "ashby"},
		{"jobs.lever.co", "lever"},
		{"acme.recruitee.com", "recruitee"},
		{"careers.smartrecruiters.com", "smartrecruiters"},
		{"acme.workable.com", "workable"},
		{"linkedin.com", "unknown"},
		{"", "unknown"},
		{"GREENHOUSE.IO", "greenhouse"}, // case-insensitive
	}

	for _, tc := range cases {
		t.Run(tc.hostname, func(t *testing.T) {
			got := detectPlatform(tc.hostname)
			assert.Equal(t, tc.want, got, "hostname: %s", tc.hostname)
		})
	}
}
