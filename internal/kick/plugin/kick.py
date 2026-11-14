"""
Plugin kick do ttv-tui: o plugin kick do streamlink com o descarte do anúncio
costurado pelo Amazon IVS.

O app grava este arquivo como kick.py num diretório privado da sessão e o
entrega com --plugin-dir. O streamlink carrega o módulo como
streamlink.plugins.kick, no lugar do plugin embutido de mesmo nome.

O módulo executa o kick.py do próprio streamlink numa cópia privada, cria
subclasses das classes HLS dele e troca, só nessa cópia, o KickHLSStream que
_get_streams_live usa. O plugin exportado é a classe Kick do streamlink, sem
nenhum método copiado: login, desafio do navegador, VOD e clipe ficam iguais.

O anúncio vem marcado na playlist da live:

    #EXT-X-DATERANGE:ID="stitched-ad-<início>-<duração>",CLASS="live-video-net-stitched-ad",START-DATE=...,DURATION=...

Todo segmento cujo EXT-X-PROGRAM-DATE-TIME cai num desses intervalos, com
início e fim conhecidos, é descartado pelo writer HLS do streamlink, que
registra "Filtering out segments and pausing stream output" no primeiro
descartado e "Resuming stream output" no primeiro conteúdo depois deles, como
acontece na Twitch.

Se faltar o que este arquivo espera (uma classe renomeada, um método que
sumiu), o módulo não exporta __plugin__: o streamlink ignora o arquivo e usa o
plugin kick embutido. O motivo vai para o log numa linha que começa com
"ttv-tui plugin fallback:". Nenhuma exceção sai daqui, porque o streamlink só
trata ImportError ao carregar um plugin e qualquer outra derrubaria a CLI.

A sintaxe fica no que o Python 3.10 aceita, o mais antigo que o streamlink 8
suporta: um SyntaxError também derrubaria a CLI antes de tocar o vídeo.
"""

from __future__ import annotations

import logging
import sys


log = logging.getLogger(__name__)

FALLBACK_PREFIX = "ttv-tui plugin fallback: "

AD_CLASS = "live-video-net-stitched-ad"
AD_ID_PREFIX = "stitched-ad-"


def is_ad_daterange(daterange) -> bool:
    return (
        getattr(daterange, "classname", None) == AD_CLASS
        or str(getattr(daterange, "id", None) or "").startswith(AD_ID_PREFIX)
    )


def _has_duration(value) -> bool:
    return value is not None and value.total_seconds() >= 0


def bounded_ad_dateranges(dateranges):
    """Os anúncios da playlist com início e fim conhecidos.

    Declarações com o mesmo ID descrevem o mesmo intervalo: uma posterior pode
    trazer o fim que faltava na primeira (RFC 8216bis, 4.4.5.1). Os atributos
    são juntados por ID, e só fica o anúncio que, juntado, tem início e fim
    (END-DATE, DURATION ou PLANNED-DURATION). Sem fim conhecido, nada é
    descartado: a marca aberta valeria para todo o conteúdo seguinte.
    """
    from dataclasses import is_dataclass, replace

    merged = {}
    for d in dateranges or []:
        key = getattr(d, "id", None)
        if key is None or not is_dataclass(d):
            merged[object()] = d
            continue
        prev = merged.get(key)
        if prev is None:
            merged[key] = d
            continue
        missing = {
            f: getattr(d, f)
            for f in _MERGED_FIELDS
            if getattr(prev, f, None) is None and getattr(d, f, None) is not None
        }
        if missing:
            merged[key] = replace(prev, **missing)

    return [
        d
        for d in merged.values()
        if is_ad_daterange(d)
        and getattr(d, "start_date", None) is not None
        and (
            getattr(d, "end_date", None) is not None
            or _has_duration(getattr(d, "duration", None))
            or _has_duration(getattr(d, "planned_duration", None))
        )
    ]


_MERGED_FIELDS = ("classname", "start_date", "end_date", "duration", "planned_duration")


def _load_builtin():
    """Executa o kick.py do streamlink com o nome deste módulo, sem registrá-lo."""
    from importlib.util import module_from_spec

    import streamlink.plugins
    from streamlink.utils.module import get_finder

    paths = list(getattr(streamlink.plugins, "__path__", None) or [])
    if not paths:
        raise ImportError("streamlink.plugins has no path")
    spec = get_finder(paths[0]).find_spec(__name__)
    if spec is None or spec.loader is None:
        raise ImportError("streamlink has no built-in kick plugin")
    if spec.origin and spec.origin == globals().get("__file__"):
        raise ImportError("the built-in kick plugin resolved to this file")

    module = module_from_spec(spec)
    # Como num import comum, o módulo fica em sys.modules enquanto executa: as
    # dataclasses dele resolvem anotações por sys.modules[__module__].
    own = sys.modules.get(__name__)
    sys.modules[__name__] = module
    try:
        spec.loader.exec_module(module)
    finally:
        if own is not None:
            sys.modules[__name__] = own
        else:
            sys.modules.pop(__name__, None)
    return module


_REQUIRED = (
    ("Kick", "_get_streams_live"),
    ("KickHLSStream", "parse_variant_playlist"),
    ("KickHLSStream", "__reader__"),
    ("KickHLSStream", "__parser__"),
    ("KickHLSStreamReader", "__worker__"),
    ("KickHLSStreamReader", "__writer__"),
    ("KickHLSStreamWorker", "process_segments"),
    ("KickHLSStreamWriter", "should_filter_segment"),
    ("KickM3U8Parser", "__segment__"),
)


def _build(base):
    """Devolve a classe Kick de base com o descarte do anúncio ligado."""
    from dataclasses import dataclass, fields, is_dataclass

    from streamlink.plugin import Plugin
    from streamlink.stream.hls import M3U8

    for owner, attr in _REQUIRED:
        cls = getattr(base, owner, None)
        if not isinstance(cls, type):
            raise LookupError(f"{owner} is missing")
        if not hasattr(cls, attr):
            raise LookupError(f"{owner}.{attr} is missing")
    if not (isinstance(base.Kick, type) and issubclass(base.Kick, Plugin)):
        raise LookupError("Kick is not a plugin")
    if not callable(getattr(M3U8, "is_date_in_daterange", None)):
        raise LookupError("M3U8.is_date_in_daterange is missing")

    segment = base.KickM3U8Parser.__segment__
    if not is_dataclass(segment):
        raise LookupError("KickM3U8Parser.__segment__ is not a dataclass")
    if "ad" in {f.name for f in fields(segment)}:
        raise LookupError(f"{segment.__name__} already has an ad field")
    if "date" not in {f.name for f in fields(segment)}:
        raise LookupError(f"{segment.__name__} has no date field")

    @dataclass(kw_only=True)
    class KickAdHLSSegment(segment):
        ad: bool = False

    class KickAdM3U8Parser(base.KickM3U8Parser):
        __segment__ = KickAdHLSSegment

    class KickAdHLSStreamWorker(base.KickHLSStreamWorker):
        _ttv_tui_warned = False

        def process_segments(self, playlist, *args, **kwargs):
            # A playlist inteira já foi lida: vale até um EXT-X-DATERANGE que
            # aparece depois dos segmentos que ele cobre.
            try:
                ads = bounded_ad_dateranges(getattr(playlist, "dateranges", None))
                for seg in playlist.segments if ads else []:
                    if any(M3U8.is_date_in_daterange(seg.date, d) for d in ads):
                        seg.ad = True
            except Exception as err:  # noqa: BLE001 - na dúvida, o vídeo passa inteiro
                for seg in playlist.segments:
                    seg.ad = False
                if not self._ttv_tui_warned:
                    self._ttv_tui_warned = True
                    detail = " ".join(f"{type(err).__name__}: {err}".split())
                    log.warning("ttv-tui ad filter skipped a playlist (" + detail + ")")
            return super().process_segments(playlist, *args, **kwargs)

    class KickAdHLSStreamWriter(base.KickHLSStreamWriter):
        def should_filter_segment(self, segment, *args, **kwargs):
            if getattr(segment, "ad", False):
                return True
            return super().should_filter_segment(segment, *args, **kwargs)

    class KickAdHLSStreamReader(base.KickHLSStreamReader):
        __worker__ = KickAdHLSStreamWorker
        __writer__ = KickAdHLSStreamWriter

    class KickAdHLSStream(base.KickHLSStream):
        __reader__ = KickAdHLSStreamReader
        __parser__ = KickAdM3U8Parser

    # Só na cópia privada: _get_streams_live procura KickHLSStream nos globais
    # do módulo dele a cada chamada.
    base.KickHLSStream = KickAdHLSStream
    return base.Kick


def _install():
    try:
        plugin = _build(_load_builtin())
    except Exception as err:  # noqa: BLE001 - nada pode sair do import do plugin
        detail = " ".join(f"{type(err).__name__}: {err}".split())
        log.warning(FALLBACK_PREFIX + "ad filter off, using the built-in kick plugin (" + detail + ")")
        return None
    return plugin


_plugin = _install()
if _plugin is not None:
    __plugin__ = _plugin
