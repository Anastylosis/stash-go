package stash

import "testing"

// The traps come from real libraries: a 720-wide SD file labelled HD, a
// 960x540 file labelled 720P, square cover art labelled 8K, and a 4K file
// that must not pass for 1080p.
func TestTierOf(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want Tier
	}{
		{0, 0, TierUnknown},
		{1920, 0, TierUnknown},
		{720, 404, TierSD},
		{720, 480, TierSD},
		{960, 540, TierSD},
		{1200, 1200, TierSD},
		{1280, 720, Tier720},
		{1919, 1080, Tier720},
		{1920, 1080, Tier1080},
		{1080, 1920, Tier1080},
		{2560, 1440, Tier1440},
		{3839, 2160, Tier1440},
		{3840, 2160, Tier4K},
		{2160, 3840, Tier4K},
		{7680, 4320, Tier8K},
	} {
		if got := TierOf(tc.w, tc.h); got != tc.want {
			t.Errorf("TierOf(%d, %d) = %s, want %s", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestFileTier(t *testing.T) {
	f := &File{Width: 3840, Height: 2160}
	if got := f.Tier(); got != Tier4K {
		t.Errorf("Tier() = %s, want 4K", got)
	}
	if Tier(99).String() != "unknown" {
		t.Errorf("Tier(99).String() = %q", Tier(99).String())
	}
}
