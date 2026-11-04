#!/usr/bin/env python3
"""twtui — Twitch no terminal: navegar por categorias, assistir e conversar.

Um processo só (Textual), sem tmux e sem vídeo em sixel: o vídeo continua no
mpv em qualidade cheia, e o chat roda aqui dentro via IRC.

Teclas:
  /            filtrar a lista
  Enter        abrir categoria / assistir canal
  Ctrl+S       buscar canal por nome no Twitch inteiro
  Ctrl+R       voltar para as categorias
  Ctrl+O       reabrir o vídeo do canal atual
  Ctrl+Q       sair
  Esc          sair do campo e voltar para a lista
"""

from __future__ import annotations

import asyncio
import json
import os
import re
import ssl
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

from rich.markup import escape
from textual import on
from textual.app import App, ComposeResult
from textual.containers import Horizontal, Vertical
from textual.widgets import Footer, Header, Input, OptionList, RichLog, Static
from textual.widgets.option_list import Option

CLIENT_ID = "kimne78kx3ncx6brgo4mv6wki5h1ko"
GQL_URL = "https://gql.twitch.tv/gql"
IRC_HOST = "irc.chat.twitch.tv"
IRC_PORT = 6697
CONFIG = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config")) / "twt" / "config.toml"


# --------------------------------------------------------------------------
# Twitch: dados de navegação (GraphQL público do site, sem login)
# --------------------------------------------------------------------------
def gql(query: str, variables: dict | None = None) -> dict:
    payload: dict = {"query": query}
    if variables:
        payload["variables"] = variables
    req = urllib.request.Request(
        GQL_URL,
        data=json.dumps(payload).encode(),
        headers={"Client-Id": CLIENT_ID, "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=20) as resp:
        body = json.load(resp)
    if body.get("errors"):
        raise RuntimeError(body["errors"][0].get("message", "erro na API"))
    return body.get("data") or {}


def human(n: int) -> str:
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n / 1_000:.0f}k"
    return str(n)


def fetch_categories(limit: int = 50) -> list[dict]:
    data = gql(
        """
        query($n: Int!) {
          games(first: $n) { edges { node { id name viewersCount } } }
        }
        """,
        {"n": limit},
    )
    items = []
    for edge in data.get("games", {}).get("edges", []):
        node = edge["node"]
        viewers = node.get("viewersCount") or 0
        if viewers:
            items.append({"id": node["id"], "name": node["name"], "viewers": viewers})
    items.sort(key=lambda i: -i["viewers"])
    return items


def fetch_channels(game: str, limit: int = 60) -> list[dict]:
    data = gql(
        """
        query($name: String!, $n: Int!) {
          game(name: $name) {
            streams(first: $n) {
              edges {
                node {
                  broadcaster { login displayName }
                  viewersCount
                  title
                }
              }
            }
          }
        }
        """,
        {"name": game, "n": limit},
    )
    edges = ((data.get("game") or {}).get("streams") or {}).get("edges", [])
    rows = []
    for edge in edges:
        node = edge["node"]
        who = node["broadcaster"]
        rows.append(
            {
                "login": who["login"],
                "name": who["displayName"],
                "viewers": node.get("viewersCount") or 0,
                "title": " ".join((node.get("title") or "").split()),
                "game": game,
            }
        )
    return rows


def search_channels(text: str) -> list[dict]:
    data = gql(
        """
        query($q: String!) {
          searchFor(userQuery: $q, platform: "web") {
            channels {
              edges { item { ... on User { login displayName } } }
            }
          }
        }
        """,
        {"q": text},
    )
    edges = ((data.get("searchFor") or {}).get("channels") or {}).get("edges", [])
    rows = []
    for edge in edges[:30]:
        item = edge.get("item") or {}
        if item.get("login"):
            rows.append(
                {
                    "login": item["login"],
                    "name": item["displayName"],
                    "viewers": 0,
                    "title": "",
                    "game": "busca",
                }
            )
    return rows


# --------------------------------------------------------------------------
# Chat: IRC do Twitch (mesmo token do twt)
# --------------------------------------------------------------------------
def read_credentials() -> tuple[str, str] | None:
    if not CONFIG.exists():
        return None
    text = CONFIG.read_text()
    user = re.search(r'^username\s*=\s*"([^"]+)"', text, flags=re.M)
    token = re.search(r'^token\s*=\s*"oauth:?([^"]+)"', text, flags=re.M)
    if not user or not token:
        return None
    return user.group(1), token.group(1)


TAG_RE = re.compile(r"@([^ ]+) ")
PRIVMSG_RE = re.compile(r"^:(\w+)!.* PRIVMSG #\S+ :(.*)$")


def parse_tags(raw: str) -> dict[str, str]:
    tags = {}
    for pair in raw.split(";"):
        key, _, value = pair.partition("=")
        tags[key] = value
    return tags


class Irc:
    """Conexão IRC mínima: entrar num canal, receber e enviar mensagens."""

    def __init__(self, nick: str, token: str, on_line, on_status) -> None:
        self.nick = nick
        self.token = token
        self.on_line = on_line
        self.on_status = on_status
        self.channel = ""
        self._writer: asyncio.StreamWriter | None = None
        self._task: asyncio.Task | None = None

    async def join(self, channel: str) -> None:
        await self.close()
        self.channel = channel
        self._task = asyncio.create_task(self._run())

    async def close(self) -> None:
        if self._task:
            self._task.cancel()
            self._task = None
        if self._writer:
            self._writer.close()
            try:
                await self._writer.wait_closed()
            except Exception:
                pass
            self._writer = None

    async def say(self, text: str) -> None:
        if not self._writer or not self.channel:
            return
        self._writer.write(f"PRIVMSG #{self.channel} :{text}\r\n".encode())
        await self._writer.drain()

    async def _run(self) -> None:
        ctx = ssl.create_default_context()
        try:
            reader, writer = await asyncio.open_connection(IRC_HOST, IRC_PORT, ssl=ctx)
        except Exception as exc:
            self.on_status(f"chat fora do ar: {exc}")
            return

        self._writer = writer
        writer.write(
            f"PASS oauth:{self.token}\r\nNICK {self.nick}\r\n".encode()
        )
        writer.write(b"CAP REQ :twitch.tv/tags twitch.tv/commands\r\n")
        writer.write(f"JOIN #{self.channel}\r\n".encode())
        await writer.drain()
        self.on_status(f"conectado em #{self.channel}")

        try:
            while True:
                line = await reader.readline()
                if not line:
                    self.on_status("chat desconectado")
                    return
                text = line.decode(errors="replace").rstrip("\r\n")
                if text.startswith("PING"):
                    writer.write(b"PONG :tmi.twitch.tv\r\n")
                    await writer.drain()
                    continue
                if " PRIVMSG " in text:
                    self._emit_message(text)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            self.on_status(f"chat caiu: {exc}")

    def _emit_message(self, text: str) -> None:
        tags = {}
        if text.startswith("@"):
            tag_part, _, rest = text[1:].partition(" ")
            tags = parse_tags(tag_part)
            text = rest
        match = PRIVMSG_RE.match(text)
        if not match:
            return
        user = tags.get("display-name") or match.group(1)
        body = match.group(2)
        color = tags.get("color") or ""
        self.on_line(user, body, color)


# --------------------------------------------------------------------------
# App
# --------------------------------------------------------------------------
class Twtui(App):
    CSS = """
    Screen { layout: vertical; }
    #body { height: 1fr; }
    #sidebar { width: 42%; border-right: solid $panel; }
    #filter { dock: top; }
    #list { height: 1fr; }
    #main { width: 58%; }
    #info { height: auto; padding: 0 1; background: $panel; color: $text; }
    #chat { height: 1fr; padding: 0 1; }
    #msg { dock: bottom; }
    .muted { color: $text-muted; }
    """

    BINDINGS = [
        ("ctrl+r", "categories", "Categorias"),
        ("ctrl+s", "search", "Buscar canal"),
        ("ctrl+o", "replay", "Reabrir vídeo"),
        ("ctrl+q", "quit", "Sair"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self.mode = "categories"
        self.category = ""
        self.current: dict | None = None
        self.categories: list[dict] = []
        self.channels: list[dict] = []
        self.searching = False
        self.video: subprocess.Popen | None = None
        self.creds = read_credentials()
        self.irc: Irc | None = None
        if self.creds:
            self.irc = Irc(self.creds[0], self.creds[1], self.chat_line, self.chat_status)

    # -- layout ------------------------------------------------------------
    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Horizontal(id="body"):
            with Vertical(id="sidebar"):
                yield Input(placeholder="filtrar…  (/ para focar)", id="filter")
                yield OptionList(id="list")
            with Vertical(id="main"):
                yield Static("escolha um canal na lista", id="info")
                yield RichLog(id="chat", wrap=True, markup=True, auto_scroll=True)
                yield Input(placeholder="Enter envia · Esc volta para a lista", id="msg")
        yield Footer()

    def on_mount(self) -> None:
        self.title = "twtui"
        self.sub_title = "Twitch no terminal"
        self.query_one("#msg", Input).disabled = True
        self.load_categories()

    # -- dados -------------------------------------------------------------
    def load_categories(self) -> None:
        self.mode = "categories"
        self.category = ""
        self.searching = False
        self.query_one("#filter", Input).value = ""
        self.info("carregando categorias…")
        self.run_worker(self._categories_worker, exclusive=True)

    async def _categories_worker(self) -> None:
        try:
            self.categories = await asyncio.to_thread(fetch_categories)
        except Exception as exc:
            self.info(f"erro ao listar categorias: {exc}")
            return
        self.fill_list(
            [
                f"[cyan]{human(c['viewers']):>6}[/]  {escape(c['name'])}" for c in self.categories
            ]
        )
        self.info(f"{len(self.categories)} categorias ao vivo · Enter abre · / filtra")

    def load_channels(self, game: str) -> None:
        self.mode = "channels"
        self.category = game
        self.query_one("#filter", Input).value = ""
        self.info(f"carregando canais de {game}…")
        self.run_worker(lambda: self._channels_worker(game), exclusive=True)

    async def _channels_worker(self, game: str) -> None:
        try:
            self.channels = await asyncio.to_thread(fetch_channels, game)
        except Exception as exc:
            self.info(f"erro ao listar canais: {exc}")
            return
        self.fill_list([self.channel_label(c) for c in self.channels])
        self.info(f"{game} · {len(self.channels)} canais · Enter assiste · Esc categorias")

    def run_search(self, text: str) -> None:
        self.mode = "search"
        self.searching = True
        self.info(f"buscando {text}…")
        self.run_worker(lambda: self._search_worker(text), exclusive=True)

    async def _search_worker(self, text: str) -> None:
        try:
            rows = await asyncio.to_thread(search_channels, text)
        except Exception as exc:
            self.info(f"erro na busca: {exc}")
            return
        self.channels = rows
        self.fill_list([f"  {escape(c['name'])} [dim]{escape(c['login'])}[/]" for c in rows])
        self.info(f"busca por {text} · {len(rows)} canais · Enter assiste")

    @staticmethod
    def channel_label(c: dict) -> str:
        title = c["title"]
        if len(title) > 80:
            title = title[:77] + "…"
        viewers = f"[cyan]{human(c['viewers']):>6}[/]  " if c["viewers"] else "        "
        return f"{viewers}[bold]{escape(c['name'])}[/] [dim]{escape(title)}[/]"

    def fill_list(self, labels: list[str]) -> None:
        ol = self.query_one("#list", OptionList)
        ol.clear_options()
        ol.add_options([Option(label, id=str(i)) for i, label in enumerate(labels)])
        if labels:
            ol.highlighted = 0

    def info(self, text: str) -> None:
        self.query_one("#info", Static).update(text)

    # -- ações -------------------------------------------------------------
    @on(OptionList.OptionSelected, "#list")
    def selected(self, event: OptionList.OptionSelected) -> None:
        index = int(event.option.id or 0)
        if self.mode == "categories":
            self.load_channels(self.categories[index]["name"])
        elif self.mode == "channels":
            self.watch(self.channels[index])
        elif self.mode == "search":
            self.watch(self.channels[index])

    @on(Input.Submitted, "#filter")
    def filter_submitted(self, event: Input.Submitted) -> None:
        ol = self.query_one("#list", OptionList)
        if ol.option_count:
            ol.highlighted = 0
            ol.focus()

    @on(Input.Changed, "#filter")
    def filter_changed(self, event: Input.Changed) -> None:
        text = event.value.strip().lower()
        ol = self.query_one("#list", OptionList)
        if self.mode == "categories":
            rows = [c for c in self.categories if text in c["name"].lower()]
            labels = [f"[cyan]{human(c['viewers']):>6}[/]  {escape(c['name'])}" for c in rows]
        elif self.mode in ("channels", "search"):
            rows = [
                c
                for c in self.channels
                if text in c["name"].lower() or text in c["login"].lower() or text in c["title"].lower()
            ]
            labels = [self.channel_label(c) for c in rows]
        else:
            return
        ol.clear_options()
        ol.add_options([Option(label, id=str(i)) for i, label in enumerate(labels)])
        if labels:
            ol.highlighted = 0

    @on(Input.Submitted, "#msg")
    async def send_message(self, event: Input.Submitted) -> None:
        text = event.value.strip()
        event.input.value = ""
        if not text or not self.irc:
            return
        await self.irc.say(text)
        self.query_one("#chat", RichLog).write(f"[dim]> {escape(text)}[/]")

    def watch(self, channel: dict) -> None:
        self.current = channel
        login = channel["login"]
        self.info(
            f"[bold]▶ {escape(channel['name'])}[/] · {escape(channel['game'])}"
            + (f" · {human(channel['viewers'])} viewers" if channel["viewers"] else "")
        )

        # vídeo: encerra o anterior e abre o novo em processo separado
        if self.video and self.video.poll() is None:
            try:
                self.video.terminate()
            except Exception:
                pass
        try:
            self.video = subprocess.Popen(
                ["twitch", login],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
        except Exception as exc:
            self.query_one("#chat", RichLog).write(f"[red]não consegui abrir o vídeo: {exc}[/]")

        chat = self.query_one("#chat", RichLog)
        chat.clear()
        msg = self.query_one("#msg", Input)
        msg.disabled = False
        msg.focus()

        if self.irc:
            chat.write(f"[dim]entrando em #{login}…[/]")
            self.run_worker(self.irc.join(login), exclusive=True)
        else:
            chat.write("[yellow]sem token em ~/.config/twt/config.toml — chat desligado[/]")

    # -- callbacks do IRC (rodam no mesmo loop) -----------------------------
    def chat_line(self, user: str, body: str, color: str) -> None:
        chat = self.query_one("#chat", RichLog)
        style = f"#{color}" if color else "cyan"
        chat.write(f"[{style} bold]{escape(user)}[/] {escape(body)}")

    def chat_status(self, text: str) -> None:
        self.query_one("#chat", RichLog).write(f"[dim]{escape(text)}[/]")

    # -- binds -------------------------------------------------------------
    def action_categories(self) -> None:
        self.load_categories()

    def action_search(self) -> None:
        field = self.query_one("#filter", Input)
        text = field.value.strip()
        if text:
            self.run_search(text)
        else:
            field.focus()

    def action_replay(self) -> None:
        if self.current:
            self.watch(self.current)

    def action_quit(self) -> None:
        if self.video and self.video.poll() is None:
            try:
                self.video.terminate()
            except Exception:
                pass
        if self.irc:
            self.run_worker(self.irc.close(), exclusive=False)
        self.exit()


def main() -> int:
    if not sys.stdin.isatty():
        print("twtui precisa de um terminal interativo", file=sys.stderr)
        return 1
    Twtui().run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
