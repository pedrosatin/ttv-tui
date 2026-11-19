package components

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnrecognizedChannelKeyIsOpaque(t *testing.T) {
	secrets := []string{
		"https://user:s3cret@www.twitch.tv/gaules?token=abc&cookie=xyz",
		"https://kick.com/xqc?token=leak",
		"https://example.com/live?cookie=sess",
	}
	for _, raw := range secrets {
		key := UnrecognizedChannelKey(raw)
		if len(key) != 64 {
			t.Fatalf("key length for %q = %d, want 64 hex chars", raw, len(key))
		}
		for _, part := range []string{"user", "s3cret", "token", "abc", "cookie", "xyz", "leak", "sess", "://", "?", "@"} {
			if strings.Contains(key, part) {
				t.Fatalf("key %q for %q still contains %q", key, raw, part)
			}
		}
		if strings.Contains(key, "gaules") || strings.Contains(key, "xqc") || strings.Contains(key, "twitch") {
			t.Fatalf("key %q embeds channel text from %q", key, raw)
		}
	}
}

func TestUnrecognizedChannelKeyCanonicalizesVariants(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		same bool
	}{
		{"twitch login case", "Gaules", "gaules", true},
		{"kick label case", "kick:Xqc", "kick:xqc", true},
		{"twitch url vs login", "https://www.twitch.tv/pastedlink", "pastedlink", true},
		{"kick url vs label", "https://kick.com/xqc", "kick:xqc", true},
		{"kick www vs bare host", "https://www.kick.com/xqc", "https://kick.com/xqc", true},
		{"twitch url with query", "https://www.twitch.tv/pastedlink?token=1", "pastedlink", true},
		{"distinct platforms same name", "gaules", "kick:gaules", false},
		{"youtube video id case", "youtube:dQw4w9WgXcQ", "youtube:dqw4w9wgxcq", false},
		{"userinfo stripped to same channel", "https://u:p@twitch.tv/gaules", "gaules", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ka, kb := UnrecognizedChannelKey(c.a), UnrecognizedChannelKey(c.b)
			if (ka == kb) != c.same {
				t.Fatalf("key(%q)=%s key(%q)=%s; same=%v want %v", c.a, ka, c.b, kb, ka == kb, c.same)
			}
		})
	}
}

func TestRecordUnrecognizedExitStoresOnlyOpaqueKeys(t *testing.T) {
	dataDir := t.TempDir()
	d := NewDownloader(dataDir)
	ctx := context.Background()
	if err := saveUpdateState(dataDir, UpdateState{Trial: "8.7.0"}); err != nil {
		t.Fatal(err)
	}
	raw := "https://user:pass@www.twitch.tv/gaules?token=secret"
	ready, err := d.RecordUnrecognizedExit(ctx, "8.7.0", raw)
	if err != nil || ready {
		t.Fatalf("record = ready %v err %v", ready, err)
	}
	st := LoadUpdateState(dataDir)
	if len(st.TrialUnrecognizedChannels) != 1 {
		t.Fatalf("channels = %v, want one opaque key", st.TrialUnrecognizedChannels)
	}
	got := st.TrialUnrecognizedChannels[0]
	want := UnrecognizedChannelKey(raw)
	if got != want {
		t.Fatalf("stored %q, want %q", got, want)
	}
	rawState, err := os.ReadFile(filepath.Join(StreamlinkBaseDir(dataDir), stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"user", "pass", "token", "secret", "gaules", "://", "?"} {
		if strings.Contains(string(rawState), leak) {
			t.Fatalf("state.json contains %q: %s", leak, rawState)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawState, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestRecordUnrecognizedExitCanonicalDedup(t *testing.T) {
	dataDir := t.TempDir()
	d := NewDownloader(dataDir)
	ctx := context.Background()
	if err := saveUpdateState(dataDir, UpdateState{Trial: "8.7.0"}); err != nil {
		t.Fatal(err)
	}
	variants := []string{
		"https://www.twitch.tv/pastedlink",
		"pastedlink",
		"PastedLink",
		"https://twitch.tv/pastedlink?token=1",
	}
	for i, v := range variants {
		ready, err := d.RecordUnrecognizedExit(ctx, "8.7.0", v)
		if err != nil || ready {
			t.Fatalf("variant %d %q = ready %v err %v; want not ready", i, v, ready, err)
		}
	}
	if got := LoadUpdateState(dataDir).TrialUnrecognizedChannels; len(got) != 1 {
		t.Fatalf("channels = %v, want one after equivalent variants", got)
	}

	ready, err := d.RecordUnrecognizedExit(ctx, "8.7.0", "https://kick.com/xqc")
	if err != nil || !ready {
		t.Fatalf("distinct kick channel = ready %v err %v; want ready", ready, err)
	}
	if got := LoadUpdateState(dataDir).TrialUnrecognizedChannels; len(got) != 2 {
		t.Fatalf("channels = %v, want two", got)
	}
}

func TestRecordUnrecognizedExitStopsGrowingAtThreshold(t *testing.T) {
	dataDir := t.TempDir()
	d := NewDownloader(dataDir)
	ctx := context.Background()
	if err := saveUpdateState(dataDir, UpdateState{Trial: "8.7.0"}); err != nil {
		t.Fatal(err)
	}
	ready, err := d.RecordUnrecognizedExit(ctx, "8.7.0", "chan-a")
	if err != nil || ready {
		t.Fatalf("first = ready %v err %v", ready, err)
	}
	ready, err = d.RecordUnrecognizedExit(ctx, "8.7.0", "chan-b")
	if err != nil || !ready {
		t.Fatalf("second = ready %v err %v; want ready", ready, err)
	}
	if got := LoadUpdateState(dataDir).TrialUnrecognizedChannels; len(got) != unrecognizedExitBlameThreshold {
		t.Fatalf("at threshold = %v, want len %d", got, unrecognizedExitBlameThreshold)
	}

	// Simulate MarkStreamlinkBad failing (ErrNoPrevious): trial stays, list full.
	for _, ch := range []string{"chan-c", "chan-d", "chan-e"} {
		ready, err = d.RecordUnrecognizedExit(ctx, "8.7.0", ch)
		if err != nil || !ready {
			t.Fatalf("past threshold %q = ready %v err %v; want ready without write", ch, ready, err)
		}
	}
	if got := LoadUpdateState(dataDir).TrialUnrecognizedChannels; len(got) != unrecognizedExitBlameThreshold {
		t.Fatalf("grew past threshold: %v", got)
	}
}

func TestRecordUnrecognizedExitDropsRawLegacyEntries(t *testing.T) {
	dataDir := t.TempDir()
	d := NewDownloader(dataDir)
	ctx := context.Background()
	raw := "https://user:s3cret@www.twitch.tv/gaules?token=abc"
	// A list written before the keys were hashed: raw channel text on disk.
	if err := saveUpdateState(dataDir, UpdateState{
		Trial:                     "8.7.0",
		TrialUnrecognizedChannels: []string{"gaules", raw},
	}); err != nil {
		t.Fatal(err)
	}

	// The same channel failing again is one channel, not a second one.
	ready, err := d.RecordUnrecognizedExit(ctx, "8.7.0", "gaules")
	if err != nil || ready {
		t.Fatalf("same channel after legacy list = ready %v err %v; want not ready", ready, err)
	}
	got := LoadUpdateState(dataDir).TrialUnrecognizedChannels
	if len(got) != 1 || got[0] != UnrecognizedChannelKey("gaules") {
		t.Fatalf("list = %v, want only the opaque key of gaules", got)
	}
	data, err := os.ReadFile(stateFilePath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"s3cret", "token", "gaules"} {
		if strings.Contains(string(data), part) {
			t.Fatalf("state.json still contains %q: %s", part, data)
		}
	}
}

func TestUnrecognizedChannelKeyIgnoresUppercaseWWW(t *testing.T) {
	if UnrecognizedChannelKey("https://WWW.Example.com/live") != UnrecognizedChannelKey("https://example.com/live") {
		t.Fatal("WWW.Example.com and example.com give different keys")
	}
}
