package version

import "testing"

func TestGetBuildInfoReturnsStampedFields(t *testing.T) {
	Branch, Commit, Time = "feature/door", "abc123", "2026-08-21T00:00:00Z"
	got := GetBuildInfo()
	if got.Branch != Branch || got.Commit != Commit || got.Time != Time {
		t.Fatalf("unexpected build info: %#v", got)
	}
}
