package kick

import (
	"bytes"
	_ "embed"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// adFilterSource is the streamlink plugin that replaces the built-in kick
// plugin for a playback: the same plugin, with the segments the Amazon IVS
// marks as a stitched ad (EXT-X-DATERANGE CLASS="live-video-net-stitched-ad")
// discarded by streamlink's HLS writer, as the Twitch plugin does. When the
// streamlink in use lacks a class or method it extends, the module exports no
// plugin and streamlink plays through its own kick plugin; the reason goes to
// the log in a line starting with "ttv-tui plugin fallback:".
//
//go:embed plugin/kick.py
var adFilterSource []byte

// adFilterPluginName is the module name of the built-in plugin it replaces.
const adFilterPluginName = "kick"

// adFilterPlugin returns the plugin for one playback target. Each target gets
// its own copy of the source.
func adFilterPlugin() catalog.StreamlinkPlugin {
	return catalog.StreamlinkPlugin{Name: adFilterPluginName, Source: bytes.Clone(adFilterSource)}
}
