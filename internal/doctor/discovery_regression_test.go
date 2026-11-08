package doctor

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiscoveryDoctorReportsVideoWithoutTTY(t *testing.T) {
	base := isolate(t)
	fakeTool(t, base, "mpv")
	fakeTool(t, base, "streamlink")
	t.Setenv("PATH", base)
	var out bytes.Buffer
	Run(&out, Options{Lang: "en"})
	for _, want := range []string{"Current terminal:", "Sixel in terminal: NOT TESTED", "Sixel in mpv:", "Embedded video:", "Next step:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("terminal control in redirected report")
	}
}
