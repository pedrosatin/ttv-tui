package catalog

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/platform"
)

func TestStableID(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		id       string
		login    string
		want     string
	}{
		{
			name:     "Twitch login preferred",
			platform: platform.Twitch,
			id:       "12345",
			login:    "gaules",
			want:     "gaules",
		},
		{
			name:     "Twitch id without login",
			platform: platform.Twitch,
			id:       "12345",
			login:    "",
			want:     "12345",
		},
		{
			name:     "Kick with prefix in id",
			platform: platform.Kick,
			id:       "kick:xqc",
			login:    "",
			want:     "xqc",
		},
		{
			name:     "Kick with uppercase prefix",
			platform: platform.Kick,
			id:       "KICK:xqc",
			login:    "",
			want:     "xqc",
		},
		{
			name:     "Kick login preferred",
			platform: platform.Kick,
			id:       "kick:old_slug",
			login:    "new_slug",
			want:     "new_slug",
		},
		{
			name:     "Kick with prefix in login",
			platform: platform.Kick,
			id:       "123",
			login:    "kick:xqc",
			want:     "xqc",
		},
		{
			name:     "Kick with uppercase prefix in login",
			platform: platform.Kick,
			id:       "123",
			login:    "KICK:xqc",
			want:     "xqc",
		},
		{
			name:     "YouTube with prefix",
			platform: platform.YouTube,
			id:       "youtube:@ChannelName",
			login:    "",
			want:     "@ChannelName",
		},
		{
			name:     "Empty platform normalized to Twitch",
			platform: "",
			id:       "twitch:user",
			login:    "",
			want:     "user",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StableID(tc.platform, tc.id, tc.login)
			if got != tc.want {
				t.Fatalf("StableID(%q, %q, %q) = %q, want %q", tc.platform, tc.id, tc.login, got, tc.want)
			}
			ch := Channel{
				Platform: tc.platform,
				ID:       tc.id,
				Login:    tc.login,
			}
			if ch.StableID() != tc.want {
				t.Fatalf("ch.StableID() = %q, want %q", ch.StableID(), tc.want)
			}
		})
	}
}

func TestSameChannel(t *testing.T) {
	t.Run("cross platform distinction", func(t *testing.T) {
		tw := Channel{Platform: platform.Twitch, Login: "gaules"}
		kick := Channel{Platform: platform.Kick, Login: "gaules"}
		yt := Channel{Platform: platform.YouTube, Login: "gaules"}
		def := Channel{Platform: "", Login: "gaules"}

		if SameChannel(tw, kick) {
			t.Fatal("Twitch and Kick channels with same login must not match")
		}
		if SameChannel(kick, yt) {
			t.Fatal("Kick and YouTube channels with same login must not match")
		}
		if !SameChannel(tw, def) {
			t.Fatal("Empty platform must normalize to Twitch")
		}
		if !tw.Matches(def) {
			t.Fatal("ch.Matches must behave like SameChannel")
		}
	})

	t.Run("Twitch case insensitivity and ID matching", func(t *testing.T) {
		a := Channel{Platform: platform.Twitch, ID: "1", Login: "alanzoka"}
		b := Channel{Platform: platform.Twitch, ID: "1", Login: "ALANZOKA"}
		c := Channel{Platform: platform.Twitch, ID: "2", Login: "alanzoka"}
		d := Channel{Platform: platform.Twitch, ID: "1", Login: ""}

		if !SameChannel(a, b) {
			t.Fatal("Twitch matching by login must be case-insensitive")
		}
		if !SameChannel(a, c) {
			t.Fatal("Twitch matching by same login must match even if ID differs")
		}
		if !SameChannel(a, d) {
			t.Fatal("Twitch matching by same non-empty ID must match")
		}
	})

	t.Run("YouTube case sensitivity", func(t *testing.T) {
		a := Channel{Platform: platform.YouTube, Login: "dQw4w9WgXcQ"}
		b := Channel{Platform: platform.YouTube, Login: "dQw4w9WgXcQ"}
		c := Channel{Platform: platform.YouTube, Login: "DQW4W9WGXCQ"}

		if !SameChannel(a, b) {
			t.Fatal("YouTube exact case match must match")
		}
		if SameChannel(a, c) {
			t.Fatal("YouTube comparison must be case-sensitive")
		}
	})

	t.Run("Kick case insensitivity", func(t *testing.T) {
		a := Channel{Platform: platform.Kick, Login: "xqc"}
		b := Channel{Platform: platform.Kick, Login: "XQC"}
		c := Channel{Platform: platform.Kick, ID: "kick:xqc"}
		d := Channel{Platform: platform.Kick, Login: "other"}

		if !SameChannel(a, b) {
			t.Fatal("Kick comparison must be case-insensitive")
		}
		if !SameChannel(a, c) {
			t.Fatal("Kick login should match kick: prefix id")
		}
		if SameChannel(a, d) {
			t.Fatal("Different Kick channels must not match")
		}
	})

	t.Run("missing identifiers", func(t *testing.T) {
		a := Channel{Platform: platform.Kick}
		b := Channel{Platform: platform.Kick}
		if SameChannel(a, b) {
			t.Fatal("Empty Kick channels must not match")
		}

		twA := Channel{Platform: platform.Twitch}
		twB := Channel{Platform: platform.Twitch}
		if SameChannel(twA, twB) {
			t.Fatal("Empty Twitch channels must not match")
		}
	})
}

func TestPlayerLabel(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		login    string
		want     string
	}{
		{
			name:     "Twitch returns login",
			platform: platform.Twitch,
			login:    "gaules",
			want:     "gaules",
		},
		{
			name:     "Default returns login",
			platform: "",
			login:    "gaules",
			want:     "gaules",
		},
		{
			name:     "Kick qualified label",
			platform: platform.Kick,
			login:    "xqc",
			want:     "kick:xqc",
		},
		{
			name:     "Kick without login returns platform",
			platform: platform.Kick,
			login:    "",
			want:     "kick",
		},
		{
			name:     "YouTube qualified label",
			platform: platform.YouTube,
			login:    "@Veritasium",
			want:     "youtube:@Veritasium",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := Channel{Platform: tc.platform, Login: tc.login}
			if got := ch.PlayerLabel(); got != tc.want {
				t.Fatalf("PlayerLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLanguageHelpers(t *testing.T) {
	if got := NormalizeLanguage("  PT  "); got != "pt" {
		t.Fatalf("NormalizeLanguage = %q, want %q", got, "pt")
	}
	if got := NormalizeLanguage(""); got != "" {
		t.Fatalf("NormalizeLanguage(\"\") = %q, want empty", got)
	}

	if ptr := LanguagePtr("   "); ptr != nil {
		t.Fatalf("LanguagePtr(\"   \") = %v, want nil", ptr)
	}
	if ptr := LanguagePtr("EN"); ptr == nil || *ptr != "en" {
		t.Fatalf("LanguagePtr(\"EN\") = %v, want \"en\"", ptr)
	}

	intVal := 42
	if ptr := IntPtr(intVal); ptr == nil || *ptr != intVal {
		t.Fatalf("IntPtr(%d) = %v, want %d", intVal, ptr, intVal)
	}

	strVal := "hello"
	if ptr := StringPtr(strVal); ptr == nil || *ptr != strVal {
		t.Fatalf("StringPtr(%q) = %v, want %q", strVal, ptr, strVal)
	}
}
