package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Compila a ponte com doubles das APIs do core, sem abrir SDL, GPU ou mpv.
func TestRenderCoreCallsAreQueued(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("os doubles dependem de LD_PRELOAD, gl.pc e do ld do GNU")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(src, []byte(`
#include "bridge.h"
#include <stdio.h>
#include <string.h>
static int queued;
static int properties;
int mpv_command(mpv_handle *m, const char **args) {
    fputs("load_fd chamou mpv_command sincrono\n", stderr);
    return -1;
}
int mpv_command_async(mpv_handle *m, uint64_t id, const char **args) {
    if (!strcmp(args[0], "loadfile") && !strcmp(args[1], "fdclose://42") && !args[2]) queued++;
    return 0;
}
int mpv_set_property_async(mpv_handle *m, uint64_t id, const char *name, mpv_format format, void *data) {
    if (format != MPV_FORMAT_STRING) return -1;
    const char *value = *(const char **)data;
    if ((!strcmp(name, "video-margin-ratio-left") && !strcmp(value, "0.12500")) ||
        (!strcmp(name, "osd-align-x") && !strcmp(value, "right")) ||
        (!strcmp(name, "osd-margin-x") && !strcmp(value, "96"))) properties++;
    return 0;
}
int main(void) {
    load_fd(NULL, "fdclose://42");
    if (queued != 1) { fputs("loadfile nao foi enfileirado\n", stderr); return 1; }
    set_property(NULL, "video-margin-ratio-left", "0.12500");
    set_property(NULL, "osd-align-x", "right");
    set_property(NULL, "osd-margin-x", "96");
    if (properties != 3) { fputs("propriedades nao foram enfileiradas\n", stderr); return 1; }
    return 0;
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	flags, err := exec.Command("pkg-config", "--cflags", "sdl3", "mpv", "gl").Output()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append(strings.Fields(string(flags)), "-I.", "-ffunction-sections", "-fdata-sections", "bridge.c", src, "-Wl,--gc-sections", "-o", bin)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}
