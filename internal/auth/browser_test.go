package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseINISections_FirefoxProfiles(t *testing.T) {
	input := `[General]
StartWithLastProfile=1

[Profile0]
Name=default-release
IsRelative=1
Path=abcd.default-release
Default=1

[Profile1]
Name=dev
IsRelative=0
Path=/tmp/ff-dev
`
	secs := parseINISections(input)
	if len(secs) != 3 {
		t.Fatalf("sections = %d", len(secs))
	}
	if secs[1].values["Name"] != "default-release" {
		t.Fatalf("name = %q", secs[1].values["Name"])
	}
	if secs[2].values["IsRelative"] != "0" {
		t.Fatalf("IsRelative = %q", secs[2].values["IsRelative"])
	}
}

func TestFindChromiumCookies_PrefersNetwork(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "Cookies")
	networkDir := filepath.Join(dir, "Network")
	if err := os.MkdirAll(networkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	network := filepath.Join(networkDir, "Cookies")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(network, []byte("network"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findChromiumCookies(dir)
	if got != network {
		t.Fatalf("got %q want %q", got, network)
	}
}

func TestFindChromiumCookies_LegacyFallback(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "Cookies")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findChromiumCookies(dir)
	if got != legacy {
		t.Fatalf("got %q want %q", got, legacy)
	}
}

func TestListProfiles_Smoke(t *testing.T) {
	profiles, err := ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	// This machine is known to have Brave; do not fail hard if CI has none.
	t.Logf("found %d profiles", len(profiles))
	for _, p := range profiles {
		t.Logf("- %s / %s (%s)", p.Browser, p.Name, p.CookiesPath)
		if p.CookiesPath == "" {
			t.Fatalf("empty cookies path for %+v", p)
		}
		if p.Family != "chromium" && p.Family != "firefox" {
			t.Fatalf("bad family %q", p.Family)
		}
	}
}
