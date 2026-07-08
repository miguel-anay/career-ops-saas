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

// TestLookupAllowedHost tests the SSRF allowlist gate.
// No mocks needed — pure regex-based hostname matching.
func TestLookupAllowedHost(t *testing.T) {
	cases := []struct {
		hostname string
		allowed  bool
	}{
		{"www.linkedin.com", true},
		{"linkedin.com", true},
		{"pe.indeed.com", true},
		{"www.indeed.com", true},
		{"indeed.com", true},
		{"computrabajo.com.pe", true},
		{"www.computrabajo.com.pe", true},
		{"computrabajo.com", true},
		{"bumeran.com.pe", true},
		{"www.bumeran.com.ar", true},
		{"bumeran.com", true},
		{"boards.greenhouse.io", false},  // ATS, not in allowlist
		{"jobs.lever.co", false},         // ATS, not in allowlist
		{"example.com", false},           // non-allowlisted
		{"evil.com", false},              // SSRF target
		{"192.168.1.1", false},           // internal IP
		{"localhost", false},             // loopback
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.hostname, func(t *testing.T) {
			_, ok := lookupAllowedHost(tc.hostname)
			assert.Equal(t, tc.allowed, ok, "hostname: %s", tc.hostname)
		})
	}
}
