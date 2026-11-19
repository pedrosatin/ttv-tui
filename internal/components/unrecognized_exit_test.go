package components

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordUnrecognizedExitNeedsTwoDistinctChannels(t *testing.T) {
	dataDir := t.TempDir()
	d := NewDownloader(dataDir)
	ctx := context.Background()
	if err := saveUpdateState(dataDir, UpdateState{Trial: "8.7.0"}); err != nil {
		t.Fatal(err)
	}

	ready, err := d.RecordUnrecognizedExit(ctx, "8.7.0", "chan-a")
	if err != nil || ready {
		t.Fatalf("first channel = ready %v, err %v; want not ready", ready, err)
	}
	ready, err = d.RecordUnrecognizedExit(ctx, "8.7.0", "chan-a")
	if err != nil || ready {
		t.Fatalf("same channel again = ready %v, err %v; want not ready", ready, err)
	}
	if got := LoadUpdateState(dataDir).TrialUnrecognizedChannels; len(got) != 1 || got[0] != "chan-a" {
		t.Fatalf("channels = %v, want [chan-a]", got)
	}

	ready, err = d.RecordUnrecognizedExit(ctx, "8.7.0", "chan-b")
	if err != nil || !ready {
		t.Fatalf("second channel = ready %v, err %v; want ready", ready, err)
	}
	if got := LoadUpdateState(dataDir).TrialUnrecognizedChannels; len(got) != 2 {
		t.Fatalf("channels = %v, want two", got)
	}

	if _, err := d.RecordUnrecognizedExit(ctx, "9.0.0", "chan-c"); !errors.Is(err, ErrNotOnTrial) {
		t.Fatalf("wrong version = %v, want ErrNotOnTrial", err)
	}
}

func TestLoadUpdateStateIgnoresMissingUnrecognizedChannels(t *testing.T) {
	dataDir := t.TempDir()
	baseDir := StreamlinkBaseDir(dataDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Old format without trial_unrecognized_channels.
	raw, err := json.Marshal(map[string]any{
		"last_check": "2026-11-12T09:00:00Z",
		"trial":      "8.7.0",
		"bad":        []string{"8.6.9"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, stateFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	st := LoadUpdateState(dataDir)
	if st.Trial != "8.7.0" || !st.IsBad("8.6.9") || len(st.TrialUnrecognizedChannels) != 0 {
		t.Fatalf("migrated state = %+v", st)
	}
}

func TestConfirmStreamlinkClearsUnrecognizedChannels(t *testing.T) {
	dataDir := t.TempDir()
	d := NewDownloader(dataDir)
	if err := saveUpdateState(dataDir, UpdateState{
		Trial:                     "8.7.0",
		TrialUnrecognizedChannels: []string{"chan-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.ConfirmStreamlink(context.Background(), "8.7.0"); err != nil {
		t.Fatal(err)
	}
	st := LoadUpdateState(dataDir)
	if st.Trial != "" || len(st.TrialUnrecognizedChannels) != 0 {
		t.Fatalf("after confirm = %+v", st)
	}
}
