"""Roda o plugin kick do ttv-tui dentro do streamlink instalado, sem rede.

Uso: adfilter_harness.py <modo> <diretório do plugin> [diretório de trabalho]

Modos:
  filter          live com anúncio marcado numa playlist servida em 127.0.0.1;
                  imprime em stdout os bytes que o streamlink entregou.
  builtin         a mesma live pelo plugin kick embutido, sem o do ttv-tui:
                  controle de que o anúncio passa sem o filtro (o diretório do
                  plugin é ignorado).
  fallback-class  o kick.py embutido sem KickHLSStreamWriter (streamlink que
                  mudou por dentro); o plugin tem de ceder ao embutido.
  fallback-none   o streamlink sem kick.py embutido.

O log sai em stderr no formato da CLI do streamlink ("[{name}][{levelname}]
{message}"), o mesmo que o app lê. A última linha de stdout é "OK" quando as
checagens daqui passam, ou "SKIP <motivo>" quando o streamlink não é o 8.x que
o plugin estende; o teste em Go confere o resto.
"""

from __future__ import annotations

import http.server
import inspect
import os
import sys
import threading
from pathlib import Path

import streamlink.plugins
from streamlink import Streamlink
from streamlink import logger as sl_logger


CONTENT = [b"content-0|", b"content-1|", b"content-2|", b"content-3|"]
ADS = [b"ad-0|", b"ad-1|"]


def media_playlist() -> str:
    lines = [
        "#EXTM3U",
        "#EXT-X-VERSION:3",
        "#EXT-X-TARGETDURATION:2",
        "#EXT-X-MEDIA-SEQUENCE:0",
        "#EXT-X-PLAYLIST-TYPE:VOD",
        "#EXT-X-PROGRAM-DATE-TIME:2026-11-14T12:00:00.000Z",
        "#EXTINF:2.000,live",
        "/seg/content-0.ts",
        "#EXT-X-PROGRAM-DATE-TIME:2026-11-14T12:00:02.000Z",
        "#EXTINF:2.000,live",
        "/seg/content-1.ts",
        # As três marcas que o IVS põe antes do anúncio (guia do IVS, "SSAI
        # Playlist Reference").
        '#EXT-X-DATERANGE:ID="stitched-ad-1794657604-4000000000",CLASS="live-video-net-stitched-ad",'
        'START-DATE="2026-11-14T12:00:04.000Z",DURATION=4.000,X-NET-LIVE-VIDEO-AD-AD-BREAK-ID="ttv-tui-test"',
        '#EXT-X-DATERANGE:ID="source-1794657604",CLASS="live-video-net-stream-source",'
        'START-DATE="2026-11-14T12:00:04.000Z",END-ON-NEXT=YES,X-NET-LIVE-VIDEO-STREAM-SOURCE="0f262e65"',
        "#EXT-X-DISCONTINUITY",
        "#EXT-X-PROGRAM-DATE-TIME:2026-11-14T12:00:04.000Z",
        "#EXTINF:2.000,0f262e65-a709-4ef1-8741-e82d936c",
        "/seg/ad-0.ts",
        "#EXT-X-PROGRAM-DATE-TIME:2026-11-14T12:00:06.000Z",
        "#EXTINF:2.000,0f262e65-a709-4ef1-8741-e82d936c",
        "/seg/ad-1.ts",
        '#EXT-X-DATERANGE:ID="source-1794657608",CLASS="live-video-net-stream-source",'
        'START-DATE="2026-11-14T12:00:08.000Z",END-ON-NEXT=YES,X-NET-LIVE-VIDEO-STREAM-SOURCE="live"',
        "#EXT-X-DISCONTINUITY",
        "#EXT-X-PROGRAM-DATE-TIME:2026-11-14T12:00:08.000Z",
        "#EXTINF:2.000,live",
        "/seg/content-2.ts",
        "#EXT-X-PROGRAM-DATE-TIME:2026-11-14T12:00:10.000Z",
        "#EXTINF:2.000,live",
        "/seg/content-3.ts",
        "#EXT-X-ENDLIST",
    ]
    return "\n".join(lines) + "\n"


MASTER = "\n".join([
    "#EXTM3U",
    '#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,FRAME-RATE=30.000',
    "/720p30.m3u8",
]) + "\n"


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):  # noqa: N802
        if self.path == "/master.m3u8":
            body, ctype = MASTER.encode(), "application/vnd.apple.mpegurl"
        elif self.path == "/720p30.m3u8":
            body, ctype = media_playlist().encode(), "application/vnd.apple.mpegurl"
        elif self.path.startswith("/seg/") and self.path.endswith(".ts"):
            name = self.path[len("/seg/"):-len(".ts")]
            body, ctype = (name + "|").encode(), "video/mp2t"
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


def setup_logging() -> None:
    # O formato que a CLI usa com --loglevel info (streamlink_cli.main.setup_logger).
    sl_logger.basicConfig(format="[{name}][{levelname}] {message}", style="{", level="info", stream=sys.stderr)


def fail(msg: str) -> None:
    print("FAIL " + msg)
    sys.exit(1)


def run_filter(plugin_dir: str | None) -> None:
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base_url = f"http://127.0.0.1:{server.server_address[1]}"

    session = Streamlink()
    if plugin_dir is not None and not session.plugins.load_path(plugin_dir):
        fail("the ttv-tui kick plugin did not load")
    found = session.plugins.match_url("https://kick.com/ttvtuitest")
    if not found or found[0] != "kick":
        fail(f"kick.com did not match the kick plugin: {found!r}")
    plugin_cls = found[1]
    want_stream = "KickAdHLSStream" if plugin_dir is not None else "KickHLSStream"
    # _get_streams_live procura KickHLSStream nos globais do módulo dele, a cópia
    # privada do kick.py embutido em que o plugin do ttv-tui trocou a classe.
    stream_cls = plugin_cls._get_streams_live.__globals__.get("KickHLSStream")
    if getattr(stream_cls, "__name__", "") != want_stream:
        fail(f"_get_streams_live uses {stream_cls!r}, want {want_stream}")

    plugin = plugin_cls(session, "https://kick.com/ttvtuitest")
    # A resposta da API da Kick, sem rede: o resto de _get_streams_live é o do streamlink.
    plugin._query_api = lambda *a, **k: (base_url + "/master.m3u8", 1, "Just Chatting", "ttv-tui test")
    streams = plugin.streams()
    if "best" not in streams:
        fail(f"no best stream: {sorted(streams)!r}")
    fd = streams["best"].open()
    data = b""
    try:
        while True:
            chunk = fd.read(8192)
            if not chunk:
                break
            data += chunk
    finally:
        fd.close()
        server.shutdown()
    sys.stdout.write("DATA " + data.decode() + "\n")
    want = b"".join(CONTENT)
    if plugin_dir is None:
        want = b"".join(CONTENT[:2] + ADS + CONTENT[2:])
    if data != want:
        fail("streamlink wrote " + repr(data))


def run_fallback(plugin_dir: str, workdir: str, mode: str) -> None:
    real_path = list(streamlink.plugins.__path__)
    real_kick = Path(real_path[0]) / "kick.py"
    fake = Path(workdir) / "fake-builtin"
    fake.mkdir()
    if mode == "fallback-class":
        # Um streamlink que renomeou a classe que este plugin estende.
        src = real_kick.read_text(encoding="utf-8")
        (fake / "kick.py").write_text(src.replace("KickHLSStreamWriter", "KickHLSWriterRenamed"), encoding="utf-8")

    session = Streamlink()
    streamlink.plugins.__path__ = [str(fake)]
    try:
        loaded = session.plugins.load_path(plugin_dir)
    finally:
        streamlink.plugins.__path__ = real_path
    if loaded or "kick" in session.plugins:
        fail("the ttv-tui plugin replaced kick even though it could not build the ad filter")

    found = session.plugins.match_url("https://kick.com/ttvtuitest")
    if not found or found[0] != "kick":
        fail(f"kick.com did not match the built-in kick plugin: {found!r}")
    origin = Path(inspect.getfile(found[1]))
    if not origin.samefile(real_kick):
        fail(f"kick.com played through {origin}, want the built-in {real_kick}")
    print("PLUGIN " + str(origin))


def main() -> None:
    mode, plugin_dir = sys.argv[1], sys.argv[2]
    major = streamlink.__version__.split(".")[0]
    if not major.isdigit() or int(major) < 8:
        print(f"SKIP streamlink {streamlink.__version__} is older than 8.0")
        sys.stdout.flush()
        os._exit(0)
    setup_logging()
    if mode == "filter":
        run_filter(plugin_dir)
    elif mode == "builtin":
        run_filter(None)
    elif mode in ("fallback-class", "fallback-none"):
        run_fallback(plugin_dir, sys.argv[3], mode)
    else:
        fail("unknown mode " + mode)
    print("OK")
    sys.stdout.flush()
    os._exit(0)


if __name__ == "__main__":
    main()
