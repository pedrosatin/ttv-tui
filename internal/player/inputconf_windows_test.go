package player

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkOrSkip needs SeCreateSymbolicLinkPrivilege or developer mode.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

// junctionOrSkip makes a junction, which needs no privilege at all: the reason
// symlink checks alone are not enough on Windows.
func junctionOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create junctions here: %v: %s", err, out)
	}
	fi, err := os.Lstat(link)
	if err != nil || !isReparsePoint(fi) {
		t.Skipf("mklink /J made no reparse point: %v, %v", fi, err)
	}
}

func TestWriteInputConfInsideLocalAppData(t *testing.T) {
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	dir := filepath.Join(local, "ttv-tui", "run", "1-123")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Go reports no real permissions here: the check cannot be about them.
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o777 {
		t.Fatalf("directory mode = %v, %v; want the synthetic 0777", fi, err)
	}
	path := inputConfPath(dir)
	if err := writeInputConf(path); err != nil {
		t.Fatalf("writeInputConf: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != inputConfContent() {
		t.Fatalf("content = %q", got)
	}
}

// The same folder spelled differently is still the same folder.
func TestPrivacyComparesIdentityNotPathText(t *testing.T) {
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", strings.ToUpper(local))
	dir := filepath.Join(local, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := writeInputConf(inputConfPath(dir)); err != nil {
		t.Fatalf("writeInputConf: %v", err)
	}
}

func TestWriteInputConfRefusesDirOutsideLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	path := inputConfPath(dir)
	err := writeInputConf(path)
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("writeInputConf = %v, want a refusal for being outside", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("file left behind: %v", err)
	}
}

func TestWriteInputConfRefusesMissingLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "gone"))
	if err := writeInputConf(inputConfPath(privateDirIn(t, t.TempDir()))); err == nil {
		t.Fatal("accepted with no private root to compare against")
	}
}

func privateDirIn(t *testing.T, parent string) string {
	t.Helper()
	dir := filepath.Join(parent, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	return dir
}

// outsideAndLink returns a directory outside LOCALAPPDATA and a path inside it
// where the test puts a link to that directory.
func outsideAndLink(t *testing.T) (outside, link string) {
	t.Helper()
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	return t.TempDir(), filepath.Join(local, "link")
}

func assertRefusedWithoutWriting(t *testing.T, path, outside string) {
	t.Helper()
	if err := writeInputConf(path); err == nil {
		t.Fatal("writeInputConf went through a link out of the private folder")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("wrote outside the private folder: %v", entries)
	}
}

func TestWriteInputConfRefusesSymlinkedSessionDir(t *testing.T) {
	outside, link := outsideAndLink(t)
	symlinkOrSkip(t, outside, link)
	assertRefusedWithoutWriting(t, inputConfPath(link), outside)
}

func TestWriteInputConfRefusesJunctionSessionDir(t *testing.T) {
	outside, link := outsideAndLink(t)
	junctionOrSkip(t, outside, link)
	assertRefusedWithoutWriting(t, inputConfPath(link), outside)
}

// A link higher up the path escapes just the same as one on the last component.
func TestWriteInputConfRefusesLinkAboveSessionDir(t *testing.T) {
	outside, link := outsideAndLink(t)
	symlinkOrSkip(t, outside, link)
	inner := filepath.Join(outside, "session")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	path := inputConfPath(filepath.Join(link, "session"))
	if err := writeInputConf(path); err == nil {
		t.Fatal("writeInputConf went through a link above the session directory")
	}
	if _, err := os.Lstat(inputConfPath(inner)); !os.IsNotExist(err) {
		t.Fatalf("file written through the link: %v", err)
	}
}

func TestWriteInputConfRefusesSymlinkAtPath(t *testing.T) {
	dir := privateDir(t)
	target := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := inputConfPath(dir)
	symlinkOrSkip(t, target, path)
	if err := writeInputConf(path); err == nil {
		t.Fatal("writeInputConf accepted a symlink at its path")
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("symlink target overwritten: %q", got)
	}
}

// A dangling link is the classic way past a stat-then-write check.
func TestWriteInputConfRefusesDanglingSymlinkAtPath(t *testing.T) {
	dir := privateDir(t)
	target := filepath.Join(t.TempDir(), "not-yet")
	path := inputConfPath(dir)
	symlinkOrSkip(t, target, path)
	if err := writeInputConf(path); err == nil {
		t.Fatal("writeInputConf accepted a dangling symlink at its path")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target created through the link: %v", err)
	}
}

func TestWriteInputConfReplacesLeftoverFile(t *testing.T) {
	path := inputConfPath(privateDir(t))
	if err := os.WriteFile(path, []byte("quit"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := writeInputConf(path); err != nil {
		t.Fatalf("writeInputConf: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != inputConfContent() {
		t.Fatalf("content = %q", got)
	}
}
