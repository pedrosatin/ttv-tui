package docs_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The install instructions must follow the latest release without anyone
// editing them after each tag. Only the "script options" paragraph may show a
// pinned version, as an example.
var (
	// pinnedInstall matches the commands and links that pin a version.
	pinnedInstall = regexp.MustCompile(`@v?\d+\.\d+\.\d+|releases/tag/|-Version\b|install\.sh\s+v?\d`)
	versionNumber = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)
)

type readme struct {
	path, install, options string // options is the paragraph that may pin a version
	example                string // word that marks the pinned version as an example
}

var readmes = []readme{
	{"../README.md", "## Install", "**Install script options.**", "example"},
	{"../docs/README.pt-BR.md", "## Instalação", "**Opções do script.**", "exemplo"},
}

// section returns the text from the heading up to the next level-2 heading.
func section(doc, heading string) (string, bool) {
	start := strings.Index(doc, "\n"+heading+"\n")
	if start < 0 {
		return "", false
	}
	rest := doc[start+len(heading)+2:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest, true
}

// splitOptions separates the options paragraph from the rest of the section.
func splitOptions(sec, marker string) (main, options string, ok bool) {
	start := strings.Index(sec, marker)
	if start < 0 {
		return "", "", false
	}
	end := strings.Index(sec[start:], "\n\n")
	if end < 0 {
		end = len(sec) - start
	}
	return sec[:start] + sec[start+end:], sec[start : start+end], true
}

// problems lists what is wrong with an install section. tags may be empty
// when the checkout has no tags.
func problems(r readme, doc string, tags map[string]bool, latest string) []string {
	var out []string
	sec, ok := section(doc, r.install)
	if !ok {
		return []string{"section " + r.install + " not found"}
	}
	main, options, ok := splitOptions(sec, r.options)
	if !ok {
		return []string{"options paragraph " + r.options + " not found"}
	}
	for _, m := range pinnedInstall.FindAllString(main, -1) {
		msg := "pinned version outside the options paragraph: " + m
		if latest != "" {
			msg += " (latest tag is " + latest + ")"
		}
		out = append(out, msg)
	}
	for _, want := range []string{"@latest", "releases/latest"} {
		if !strings.Contains(main, want) {
			out = append(out, "install instructions lack "+want)
		}
	}
	if !strings.Contains(strings.ToLower(options), r.example) {
		out = append(out, "options paragraph does not mark its versions as an example (word: "+r.example+")")
	}
	if len(tags) > 0 {
		for _, m := range versionNumber.FindAllStringSubmatch(options, -1) {
			if !tags["v"+m[1]] {
				out = append(out, "options example cites "+m[0]+", which is not a release tag")
			}
		}
	}
	return out
}

// releaseTags returns the v* tags of this checkout and the newest one.
func releaseTags(t *testing.T) (map[string]bool, string) {
	t.Helper()
	raw, err := exec.Command("git", "tag", "--list", "v*", "--sort=-v:refname").Output()
	if err != nil {
		t.Logf("git tags unavailable, skipping the tag check: %v", err)
		return nil, ""
	}
	tags := map[string]bool{}
	latest := ""
	for _, tag := range strings.Fields(string(raw)) {
		if latest == "" {
			latest = tag
		}
		tags[tag] = true
	}
	return tags, latest
}

func TestREADMEInstallFollowsLatestRelease(t *testing.T) {
	tags, latest := releaseTags(t)
	for _, r := range readmes {
		raw, err := os.ReadFile(r.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range problems(r, string(raw), tags, latest) {
			t.Errorf("%s: %s", r.path, p)
		}
	}
}

func TestProblemsCatchesPinnedInstructions(t *testing.T) {
	r := readmes[0]
	good := "x\n## Install\n\n```\n.\\install.ps1\n```\n[latest](https://github.com/o/r/releases/latest)\n" +
		"go install github.com/o/r/cmd/x@latest\n\n**Install script options.** Pin one: `install.sh 0.10.2`. Only an example.\n\n## Use\n"
	if got := problems(r, good, map[string]bool{"v0.10.2": true}, "v0.10.2"); len(got) != 0 {
		t.Fatalf("valid section reported: %v", got)
	}
	for name, bad := range map[string]string{
		"powershell -Version":  strings.Replace(good, `.\install.ps1`, `.\install.ps1 -Version 0.9.0`, 1),
		"go install pinned":    strings.Replace(good, "@latest", "@v0.9.0", 1),
		"release link pinned":  strings.Replace(good, "releases/latest", "releases/tag/v0.9.0", 1),
		"example not a tag":    strings.Replace(good, "0.10.2", "9.9.9", 1),
		"no example caveat":    strings.Replace(good, "Only an example.", "", 1),
		"lacks latest link":    strings.Replace(good, "releases/latest", "releases", 1),
		"options paragraph":    strings.Replace(good, "**Install script options.**", "**Other.**", 1),
		"section without name": strings.Replace(good, "## Install", "## Setup", 1),
	} {
		if len(problems(r, bad, map[string]bool{"v0.10.2": true}, "v0.10.2")) == 0 {
			t.Errorf("%s: no problem reported", name)
		}
	}
}
