package platform

import "testing"

func TestPlatformHelpers(t *testing.T) {
	tests := []struct {
		raw        string
		wantNorm   string
		wantTwitch bool
		wantNonTwt bool
	}{
		{"", Twitch, true, false},
		{"   ", Twitch, true, false},
		{"twitch", Twitch, true, false},
		{"TWITCH", Twitch, true, false},
		{" Twitch ", Twitch, true, false},
		{"kick", Kick, false, true},
		{"Kick", Kick, false, true},
		{"youtube", YouTube, false, true},
		{"YouTube", YouTube, false, true},
		{"trovo.live", "trovo.live", false, true},
	}

	for _, tc := range tests {
		if got := Normalize(tc.raw); got != tc.wantNorm {
			t.Errorf("Normalize(%q) = %q, want %q", tc.raw, got, tc.wantNorm)
		}
		if got := IsTwitch(tc.raw); got != tc.wantTwitch {
			t.Errorf("IsTwitch(%q) = %v, want %v", tc.raw, got, tc.wantTwitch)
		}
		if got := IsNonTwitch(tc.raw); got != tc.wantNonTwt {
			t.Errorf("IsNonTwitch(%q) = %v, want %v", tc.raw, got, tc.wantNonTwt)
		}
	}
}

func TestDisplayName(t *testing.T) {
	tests := map[string]string{
		"":          "Twitch",
		"twitch":    "Twitch",
		" KICK ":    "Kick",
		"youtube":   "YouTube",
		"Vimeo.com": "vimeo.com",
	}
	for raw, want := range tests {
		if got := DisplayName(raw); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", raw, got, want)
		}
	}
}
