#!/usr/bin/env python3
"""Fluxos por papel de usuário contra a janela real, com live real e config isolado.

Compila o checkout, cria um monitor virtual no Hyprland, abre `ttv-tui-window` nele e
conduz a TUI pelo socket de teste (`serveTest` em window/main.go). Cada verificação
imprime PASS/FAIL com a evidência. Os critérios foram definidos antes de rodar; uma
falha não interrompe os demais fluxos.

Códigos de saída:
  0  todas as verificações passaram
  1  falha de produto (algum FAIL, ou a janela caiu no meio)
  2  falta pré-requisito no ambiente (Hyprland, grim, foot, rede…) ou há outra execução em curso
  3  rodada inválida (janela fora do monitor de teste, tempo esgotado, hyprctl recusou)
  4  o checkout não compilou
"""
import argparse, fcntl, glob, json, os, re, shutil, signal, socket, subprocess, sys, tempfile, time, traceback

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
MON = "TTVFLOWS"  # monitor virtual criado e removido pela bateria
CLASS = "ttvflows"  # classe da janela de teste; a do operador é "ttv-tui"
FOOT_CLASS = "ttvflows-foot"
MARK = "TTV_FLOWS_RUN"  # variável herdada por todo processo que a bateria cria
RUN = ""  # diretório desta execução
TMP = ""  # temporários da janela: a limpeza alcança até o que um SIGKILL deixa
TEST = ""  # socket de teste da janela
PROC = None  # janela em execução
# Os critérios foram calibrados numa grade de 159 colunas: 1920x1080, escala 5/3 e a
# fonte padrão de 9 pt. A fonte é ajustada à escala do monitor virtual, que vem da
# configuração do operador, para a célula ter os mesmos pixels.
CAL_W, CAL_H, CAL_SCALE, CAL_PT = 1920, 1080, 5 / 3, 9.0
results = []
BASELINE = set()
MPV_BASE = set()
made_monitor = False
made_rules = False
lock_file = None  # fica aberto: a trava dura até o processo sair


class Missing(Exception):
    """Falta algo no ambiente: nada foi testado."""


class Invalid(Exception):
    """A rodada não vale como evidência."""


class BuildFailed(Exception):
    pass


class Abort(Exception):
    """Falha de produto que impede seguir o fluxo."""


class Tee:
    def __init__(self, *outs):
        self.outs = outs

    def write(self, s):
        for o in self.outs:
            o.write(s)

    def flush(self):
        for o in self.outs:
            o.flush()


def sh(cmd):
    """Roda o comando; nas listas de processos ignora o que já existia antes do teste."""
    out = subprocess.run(cmd, shell=True, capture_output=True, text=True).stdout
    if BASELINE and "pgrep" in cmd:
        out = "".join(l + "\n" for l in out.splitlines() if l.split(" ", 1)[0] not in BASELINE)
    return out


def load1():
    return float(open("/proc/loadavg").read().split()[0])


def t(*a):
    s = socket.socket(socket.AF_UNIX)
    s.settimeout(5)
    s.connect(TEST)
    s.sendall(" ".join(a).encode() + b"\n")
    s.shutdown(socket.SHUT_WR)
    b = b""
    while True:
        d = s.recv(65536)
        if not d:
            break
        b += d
    return b.decode("utf8", "replace")


def stats():
    return json.loads(t("stats"))


def screen():
    return t("screen")


def text(s):
    return t("raw", s.encode().hex())


def ipc(path, *names):
    s = socket.socket(socket.AF_UNIX)
    s.settimeout(3)
    s.connect(path)
    f = s.makefile("rwb", buffering=0)
    out = {}
    for i, n in enumerate(names):
        f.write(json.dumps({"command": ["get_property", n], "request_id": i + 1}).encode() + b"\n")
        while True:
            m = json.loads(f.readline())
            if m.get("request_id") == i + 1:
                out[n] = m.get("data")
                break
    return out


def wait(pred, secs, step=0.2):
    end = time.monotonic() + secs
    while time.monotonic() < end:
        try:
            v = pred()
        except (OSError, ValueError, KeyError):
            v = None
        if v:
            return v
        time.sleep(step)
    return None


def check(role, name, ok, ev=""):
    results.append((role, name, bool(ok), ev))
    print(f"{'PASS' if ok else 'FAIL'} [{role}] {name} :: {ev}", flush=True)


def monitors():
    return json.loads(subprocess.run(["hyprctl", "-j", "monitors"], capture_output=True, text=True).stdout)


def clients():
    return json.loads(sh("hyprctl -j clients"))


def ours():
    return [c for c in clients() if c["class"].startswith(CLASS)]


def real_mpv_windows():
    """Janelas do mpv abertas depois do início; as que o operador já tinha não contam."""
    return [c["class"] for c in clients() if "mpv" in c["class"].lower() and c["address"] not in MPV_BASE]


def children(pid):
    pids, todo = [], [pid]
    while todo:
        p = todo.pop()
        pids.append(p)
        try:
            for t_ in os.listdir(f"/proc/{p}/task"):
                with open(f"/proc/{p}/task/{t_}/children") as f:
                    todo += [int(x) for x in f.read().split()]
        except OSError:
            pass
    return pids


def comm(pid):
    try:
        return open(f"/proc/{pid}/comm").read().strip()
    except OSError:
        return ""


def own(pid, name):
    """Processos com esse nome na árvore da instância de teste; nunca os do usuário."""
    return [p for p in children(pid) if comm(p) == name]


def rss_mb(pid):
    return round(sum(int(open(f"/proc/{p}/statm").read().split()[1]) * 4096 for p in children(pid) if os.path.exists(f"/proc/{p}/statm")) / 1e6)


def marked():
    """Processos vivos que herdaram a marca desta execução, fora este script."""
    needle = f"{MARK}={os.environ[MARK]}".encode()
    out = []
    for p in os.listdir("/proc"):
        if not p.isdigit() or int(p) == os.getpid():
            continue
        try:
            with open(f"/proc/{p}/environ", "rb") as f:
                if needle in f.read().split(b"\0"):
                    out.append(int(p))
        except OSError:
            pass
    return out


def test_monitor():
    m = [m for m in monitors() if m["name"] == MON]
    if not m:
        raise Invalid("o monitor de teste sumiu")
    return m[0]


def hypr_eval(lua):
    out = subprocess.run(["hyprctl", "eval", lua], capture_output=True, text=True).stdout.strip()
    if out != "ok":
        raise Invalid(f"hyprctl eval recusou: {out[:200]}")


def declare_rule(name=CLASS, cls=CLASS):
    """Manda a janela de teste para o monitor virtual, sem tomar o foco do operador.

    O Hyprland não remove regra de janela em tempo de execução: a anterior é
    desativada, e a limpeza desativa todas.
    """
    global made_rules
    ws = test_monitor()["activeWorkspace"]["name"]
    hypr_eval(
        "_G.ttvflows = _G.ttvflows or {}; "
        f'if _G.ttvflows["{name}"] then _G.ttvflows["{name}"]:set_enabled(false) end; '
        f'_G.ttvflows["{name}"] = hl.window_rule({{ name = "{name}", match = {{ class = "^{cls}$" }}, '
        f'workspace = "{ws} silent", no_initial_focus = true }})'
    )
    made_rules = True


def guard_monitor():
    """Janela fora do monitor de teste invalida a rodada: fecha na hora e aborta."""
    wait(lambda: ours(), 6, 0.1)
    c = ours()
    if not c or c[0]["monitor"] != test_monitor()["id"]:
        raise Invalid("janela fora do monitor de teste" if c else "a janela não abriu; veja window.out")


def launch():
    """Abre a janela com config, estado e cache só desta execução."""
    global PROC
    for d in ("cfg", "state", "cache"):
        os.makedirs(d, exist_ok=True)
    if os.path.exists(TEST):
        os.remove(TEST)
    env = dict(os.environ)
    for k in ("TTV_TUI_VIDEO_HOST", "TTV_WINDOW_SOCK"):
        env.pop(k, None)
    env.update(
        XDG_CONFIG_HOME=f"{RUN}/cfg", XDG_STATE_HOME=f"{RUN}/state", XDG_CACHE_HOME=f"{RUN}/cache", TMPDIR=TMP,
        TTV_WINDOW_APPID=CLASS, TTV_WINDOW_MUTE="1", TTV_WINDOW_TEST=TEST, TTV_WINDOW_TUI=f"{RUN}/bin/ttv-tui",
        TTV_WINDOW_PT=f"{CAL_PT * CAL_SCALE / test_monitor()['scale']:.4f}",
    )
    log = open("window.out", "ab")
    PROC = subprocess.Popen([f"{RUN}/bin/ttv-tui-window", "--lang", "en"], stdin=subprocess.DEVNULL, stdout=log, stderr=log, env=env, start_new_session=True)
    log.close()
    wait(lambda: os.path.exists(TEST), 5, 0.1)


def up():
    for d in ("cfg", "state", "cache"):  # sem recentes herdados de outra rodada
        shutil.rmtree(d, ignore_errors=True)
    declare_rule()
    launch()
    guard_monitor()
    # o título aparece antes das linhas: espera a primeira categoria selecionável
    return wait(lambda: re.search(r"▸\s+[\d.]+[kKmM]?\s+\S", screen()), 20)


def pixels(label):
    """Desvio do canto superior esquerdo: painel vazio ~0,019; com texto > 0,03."""
    p = f"px-{label}.png"
    sh(f"grim -o {MON} {p}")
    return float(sh(f"magick {p} -crop 640x300+0+36 +repage -colorspace Gray -format '%[fx:standard_deviation]' info:") or 0)


def origin():
    """Canto da janela em pixels físicos do monitor virtual."""
    c = ours()[0]
    mon = test_monitor()
    return round((c["at"][0] - mon["x"]) * mon["scale"]), round((c["at"][1] - mon["y"]) * mon["scale"])


def grab(name):
    sh(f"grim -o {MON} {name}")
    return name


def motion(a, b, x, y, w, h):
    """Quanto a região mudou entre dois screenshots (0 = idêntica)."""
    g = f"{w}x{h}+{x}+{y}"
    v = sh(f"magick {a} {b} -crop {g} +repage -compose difference -composite -colorspace Gray -format '%[fx:mean]' info:")
    return float(v or 0)


def regions():
    """Retângulos físicos: lista (esquerda), vídeo (centro) e chat (direita)."""
    s = stats()
    ox, oy = origin()
    cw, ch = s["cellW"], s["cellH"]
    col, row, cols, rows = s["hole"]
    video = (ox + (col - 1) * cw + 20, oy + 200, cols * cw - 40, rows * ch - 400)
    left = (ox + 20, oy + 200, max((col - 1) * cw - 40, 40), rows * ch - 400)
    rx = ox + (col - 1 + cols) * cw + 20
    right = (rx, oy + 200, max(ox + s["W"] - rx - 20, 40), rows * ch - 400)
    return left, video, right


def two_grabs(tag):
    a, b = grab(f"m-{tag}-a.png"), None
    time.sleep(0.5)
    b = grab(f"m-{tag}-b.png")
    return a, b


def right_column():
    return [l.rsplit("│", 1)[-1].strip() for l in screen().splitlines() if "│" in l]


def exited():
    return PROC is None or PROC.poll() is not None


def down():
    try:
        t("quit")
    except OSError:
        pass
    wait(exited, 6)
    time.sleep(0.7)


def open_channel(idx=0):
    """Da tela de categorias: entra na primeira e abre o canal idx."""
    t("key", "Return")
    wait(lambda: re.search(r"▸\s+●", screen()), 25)  # lista carregada, não só o título
    for _ in range(idx):
        t("key", "Down")
        time.sleep(0.08)
    time.sleep(0.2)
    s0 = stats()["sessions"]
    t0 = time.monotonic()
    t("key", "Return")
    got = wait(lambda: stats()["active"] and stats()["sessions"] > s0 and stats()["videoFrames"] > 0, 45, 0.1)
    return (time.monotonic() - t0) if got else None


def press(name, n, gap=0.03):
    for _ in range(n):
        t("key", name)
        time.sleep(gap)


def last_line():
    return [l for l in screen().splitlines() if l.strip()][-1]


def flows():
    # ---- Espectador casual: abrir, escolher, ver, sair
    role = "casual"
    t0 = time.monotonic()
    ok = up()
    check(role, "catálogo em até 8 s", ok and time.monotonic() - t0 < 8, f"{time.monotonic() - t0:.1f}s")
    check(role, "uma janela ao abrir", len(ours()) == 1, str(len(ours())))
    time.sleep(1.5)
    dv = pixels("boot")
    check(role, "catálogo aparece em pixels, sem vídeo", dv > 0.03, f"desvio {dv:.4f}")
    ttff = open_channel(0)
    check(role, "vídeo em até 15 s depois do Enter", ttff is not None and ttff < 15, f"{ttff and round(ttff, 1)}s")
    st = stats()
    print(f"INFO [{role}] grade de {st['cols']}x{st['rows']} células de {st['cellW']}x{st['cellH']} px", flush=True)
    check(role, "nenhuma janela do mpv", not real_mpv_windows(), str(real_mpv_windows()))
    check(role, "continua uma janela só", len(ours()) == 1, str(len(ours())))
    time.sleep(8)  # a abertura de um stream não é regime
    frames_a = stats()["videoFrames"]
    time.sleep(10)
    frames_b = stats()["videoFrames"]
    fps = (frames_b - frames_a) / 10
    check(role, "vídeo fluindo (>= 45 fps apresentados)", fps >= 45, f"{fps:.1f} fps")
    if not st["ipc"]:
        check(role, "sessão existe para seguir o fluxo", False, "sem IPC")
        tail = "\n".join(l for l in screen().splitlines() if l.strip())
        raise Abort("nenhum vídeo abriu, e os demais fluxos dependem dele. Tela:\n" + tail)
    print(f"INFO [{role}] processos com a marca da execução: {sorted(comm(p) for p in marked())}", flush=True)
    info = ipc(st["ipc"], "hwdec-current", "current-vo", "width", "height", "container-fps")
    check(role, "decode por hardware", info.get("hwdec-current") == "vaapi", str(info))
    first = screen().splitlines()[0]
    s3 = stats()
    # The list header names its platform first ("Twitch · Live Channels").
    list_title = re.sub(r"^\w+ · ", "", first.lstrip("▸ "))
    check(role, "lista à esquerda e chat à direita", list_title.startswith("Live Channels") and "Chat" in first.rsplit("│", 1)[-1], first[:60] + " … " + first.rsplit("│", 1)[-1].strip()[:30])
    check(role, "vídeo no centro", s3["holeOn"] and s3["hole"][0] > 1 and s3["hole"][0] - 1 + s3["hole"][2] < s3["cols"], str(s3["hole"]))
    L, V, R = regions()
    a, b = two_grabs("3col")
    mv, ml = motion(a, b, *V), motion(a, b, *L)
    check(role, "vídeo se mexe no centro, em pixels", mv > 0.002, f"centro {mv:.4f}")
    check(role, "lista parada à esquerda, em pixels", ml < mv / 3, f"esquerda {ml:.4f}, centro {mv:.4f}")

    # ---- Controles: pausa, volume, tela viva em pausa
    role = "controles"
    text(" ")
    paused = wait(lambda: ipc(st["ipc"], "pause")["pause"] is True, 3)
    check(role, "Espaço pausa via IPC real", paused)
    time.sleep(0.8)
    v0, p0 = stats()["videoFrames"], stats()["presents"]
    before = screen()
    t("key", "Down")
    changed = wait(lambda: screen() != before, 3)
    time.sleep(0.5)
    v1, p1 = stats()["videoFrames"], stats()["presents"]
    check(role, "lista responde com vídeo pausado", changed and p1 > p0, f"presents +{p1 - p0}, video +{v1 - v0}")
    text(" ")
    check(role, "Espaço retoma", wait(lambda: ipc(st["ipc"], "pause")["pause"] is False, 3))
    vol0 = ipc(st["ipc"], "volume")["volume"]
    text("-")
    vol1 = wait(lambda: (lambda v: v if v != vol0 else None)(ipc(st["ipc"], "volume")["volume"]), 3)
    check(role, "tecla - muda o volume", vol1 is not None and vol1 < vol0, f"{vol0} -> {vol1}")

    # ---- Chateiro: chat ao lado do vídeo, fluxo alto, sem derrubar o vídeo
    role = "chateiro"
    got = wait(lambda: len([l for l in right_column() if ":" in l and "Chat" not in l]) >= 6, 25, 0.5)
    msgs = [l for l in right_column() if ":" in l and "Chat" not in l]
    check(role, "chat visível sem atalho nenhum", got, f"{len(msgs)} linhas; ex.: {msgs[-1][:48] if msgs else ''}")
    paints0, pm0, f0 = stats()["panelPaints"], stats()["paintMs"], stats()["videoFrames"]
    line0 = screen()
    time.sleep(15)
    s1 = stats()
    paint_rate = (s1["panelPaints"] - paints0) / 15
    avg = (s1["paintMs"] - pm0) / max(1, s1["panelPaints"] - paints0)
    fps_chat = (s1["videoFrames"] - f0) / 15
    check(role, "chat rolando sozinho", screen() != line0, f"{paint_rate:.1f} repinturas/s")
    print(f"INFO [{role}] repintura fora da thread de render: {avg:.2f} ms em média, travadas {s1['stalls']}, maior intervalo {s1['maxGapMs']} ms", flush=True)
    check(role, "vídeo não cai com chat ativo", fps_chat >= 45, f"{fps_chat:.1f} fps")
    n0 = len(stats().get("latMs", []))
    for key_name in ["Down"] * 15 + ["Up"] * 15:  # ida e volta: a seleção termina onde estava
        t("key", key_name)
        time.sleep(0.15)
    time.sleep(0.3)
    lat = sorted(stats().get("latMs", [])[n0:])
    if lat:
        p50, p95 = lat[len(lat) // 2], lat[int(len(lat) * 0.95)]
        check(role, "tecla até tela < 60 ms (p50)", p50 < 60, f"p50 {p50:.0f} ms, p95 {p95:.0f} ms, n={len(lat)}, max {max(lat):.0f}")
    else:
        check(role, "tecla até tela < 60 ms (p50)", False, "nenhuma amostra de latência nas 30 teclas")

    # ---- Leitura do chat e da lista, com várias amostras ao longo do tempo
    role = "leitura"
    cut_chat, no_author, list_cut, tops = [], 0, [], []
    seen_chat, seen_list = 0, 0  # amostra sem conteúdo não aprova nada
    for _ in range(12):
        rows = screen().splitlines()
        body = [l.rsplit("│", 1)[-1].rstrip() for l in rows[2:-3] if "│" in l]
        cut_chat += [l.strip() for l in body if l.endswith("…")]
        first = next((l for l in body if l.strip()), "")
        seen_chat += bool(first)
        if first and ":" not in first and not first.lstrip().startswith("["):
            no_author += 1
            tops.append(first.strip()[:40])
        left = [l.split("│")[0].strip() for l in rows[:-1] if "│" in l]
        seen_list += any(left)
        list_cut += [l for l in left if ".…" in l or "..." in l]
        time.sleep(0.8)
    check(role, "o chat não corta palavras com reticências", seen_chat > 0 and not cut_chat, f"chat em {seen_chat} de 12 amostras; {cut_chat[:2]}")
    check(role, "o topo do chat sempre começa pelo autor", seen_chat > 0 and no_author == 0, f"{no_author} de {seen_chat} amostras{'; topo: ' + str(tops[:3]) if tops else ''}")
    check(role, "a lista usa um corte só", seen_list > 0 and not list_cut, f"lista em {seen_list} de 12 amostras; {list_cut[:2]}")

    # ---- Quem quer só o vídeo: esconder e mostrar lista e chat
    role = "toggles"
    full_cols = stats()["cols"]
    h0 = list(stats()["hole"])
    f0 = stats()["videoFrames"]
    t("key", "ctrl+h")
    ok = wait(lambda: stats()["hole"][0] == 1 and stats()["hole"][2] > h0[2], 4, 0.1)
    s4 = stats()
    check(role, "Ctrl+H esconde a lista e o vídeo cresce à esquerda", ok and "Live Channels" not in screen().splitlines()[0], str(s4["hole"]))
    time.sleep(0.5)
    a, b = two_grabs("nolist")
    L2 = (origin()[0] + 20, origin()[1] + 200, max((h0[0] - 1) * s4["cellW"] - 40, 40), s4["rows"] * s4["cellH"] - 400)
    m2 = motion(a, b, *L2)
    check(role, "o vídeo ocupa em pixels a área da lista", m2 > 0.002, f"{m2:.4f}")
    t("key", "ctrl+l")
    ok = wait(lambda: stats()["hole"][2] == full_cols, 4, 0.1)
    check(role, "Ctrl+L esconde o chat e o vídeo ocupa a largura toda", ok, str(stats()["hole"]))
    time.sleep(0.3)
    bar = last_line()
    check(role, "só com o vídeo, a barra diz como voltar", "Ctrl+H" in bar and "Ctrl+L" in bar, bar.strip()[-60:])
    time.sleep(0.3)
    check(role, "vídeo não parou ao alternar", stats()["videoFrames"] > f0 + 30, f"+{stats()['videoFrames'] - f0} quadros")
    t("key", "ctrl+h")
    time.sleep(0.25)
    t("key", "ctrl+l")
    ok = wait(lambda: stats()["hole"] == h0, 4, 0.1)
    first = screen().splitlines()[0]
    check(role, "os dois voltam ao lugar", ok and "Live Channels" in first and "Chat" in first, str(stats()["hole"]))

    # ---- Larguras: mover as divisórias
    role = "larguras"
    h0 = list(stats()["hole"])
    f0 = stats()["videoFrames"]
    press("ctrl+Right", 3)
    ok = wait(lambda: stats()["hole"] == [h0[0] + 6, 1, h0[2] - 6, h0[3]], 3, 0.05)
    check(role, "Ctrl+→ alarga a lista", ok, str(stats()["hole"]))
    list_cols = h0[0] - 2 + 6
    check(role, "a barra mostra a largura da lista", str(list_cols) in last_line(), last_line().strip()[:60])
    press("alt+Left", 3)
    ok = wait(lambda: stats()["hole"] == [h0[0] + 6, 1, h0[2] - 12, h0[3]], 3, 0.05)
    check(role, "Alt+← alarga o chat", ok, str(stats()["hole"]))
    time.sleep(0.4)
    L, V, R = regions()
    a, b = two_grabs("wider")
    mv = motion(a, b, *V)
    check(role, "vídeo continua no novo retângulo, em pixels", mv > 0.002, f"centro {mv:.4f}")
    press("ctrl+Left", 80)
    press("alt+Right", 80)
    ok = wait(lambda: stats()["hole"][:3] == [24, 1, full_cols - 22 - 24 - 2], 4, 0.05)
    check(role, "as duas param no mínimo", ok, str(stats()["hole"]))
    press("ctrl+Right", 100)
    ok = wait(lambda: stats()["hole"][2] == 32, 4, 0.05)
    s6 = stats()
    check(role, "a lista para quando o vídeo chega ao mínimo", ok and s6["holeOn"], str(s6["hole"]))
    press("alt+Left", 5)
    time.sleep(0.4)
    check(role, "o chat não esmaga o vídeo", stats()["hole"][2] == 32, str(stats()["hole"]))
    check(role, "vídeo não parou durante os ajustes", stats()["videoFrames"] > f0 + 120, f"+{stats()['videoFrames'] - f0} quadros")
    list_now, chat_now = stats()["hole"][0] - 2, full_cols - stats()["hole"][0] + 1 - stats()["hole"][2] - 1
    list_goal, chat_goal = h0[0] - 2, full_cols - h0[0] + 1 - h0[2] - 1
    press("ctrl+Left", (list_now - list_goal) // 2)
    press("alt+Left", (chat_goal - chat_now) // 2)
    ok = wait(lambda: stats()["hole"] == h0, 4, 0.05)
    check(role, "dá para voltar ao tamanho inicial", ok, str(stats()["hole"]))
    t("key", "ctrl+h")
    wait(lambda: stats()["hole"][0] == 1, 3, 0.05)
    press("ctrl+Right", 1)
    ok = wait(lambda: "hidden" in last_line().lower(), 3, 0.05)
    check(role, "com a lista oculta, o atalho avisa", ok and stats()["hole"][0] == 1, last_line().strip()[:50])
    t("key", "ctrl+h")
    wait(lambda: stats()["hole"] == h0, 3, 0.05)
    time.sleep(0.2)

    # ---- Chat sobre o vídeo
    role = "sobreposto"
    h0 = list(stats()["hole"])
    f0 = stats()["videoFrames"]
    t("key", "ctrl+o")
    ok = wait(lambda: stats()["overlay"] > 0 and stats()["hole"][2] > h0[2], 3, 0.05)
    s7 = stats()
    chat_cols = s7["overlay"]
    check(role, "Ctrl+O põe o chat sobre o vídeo e o vídeo ganha a coluna", ok and s7["hole"][2] == h0[2] + chat_cols + 1, f"{h0} -> {s7['hole']}, chat {chat_cols}")
    time.sleep(0.6)
    col, row, cc, rr = s7["hole"]
    ox, oy = origin()
    gx = ox + (col - 1 + cc - chat_cols) * s7["cellW"] + 10
    a, b = two_grabs("glass")
    mg = motion(a, b, gx, oy + 200, chat_cols * s7["cellW"] - 20, rr * s7["cellH"] - 400)
    check(role, "o vídeo aparece através do chat, em pixels", mg > 0.001, f"{mg:.4f}")
    shutil.copyfile("m-glass-b.png", "overlay.png")
    start = col - 1 + cc - chat_cols
    msgs = [l[start:].strip() for l in screen().splitlines()[:rr] if ":" in l[start:]]
    check(role, "mensagens do chat continuam visíveis", len(msgs) >= 5, f"{len(msgs)} linhas; ex.: {msgs[-1][:40] if msgs else ''}")
    # Lê os pixels direto, sem depender da posição do texto na string da tela.
    col0 = col - 1 + cc - chat_cols
    good, tried, sample = 0, 0, ""
    for y in range(3, rr - 3):
        first_a = int(t("pixel", str(col0), str(y)).split()[3])
        last_a = int(t("pixel", str(col0 + chat_cols - 1), str(y)).split()[3])
        if first_a > 0:
            tried += 1
            good += first_a >= 150 and last_a == 0
            sample = f"linha {y}: início {first_a}, fim {last_a}"
    veil_ok = tried >= 4 and good >= 3
    check(role, "o fundo fica só atrás do texto", veil_ok, f"{good} de {tried} amostras; {sample}")

    def veil_alpha():
        for y in range(3, rr - 3):
            v = int(t("pixel", str(col0), str(y)).split()[3])
            if v > 0:
                return v
        return 0

    check(role, "opacidade padrão de 70%", wait(lambda: veil_alpha() == 178, 3, 0.1), str(veil_alpha()))
    press("alt+Up", 2)
    ok = wait(lambda: stats()["overlayOpacity"] == 80 and veil_alpha() == 204, 3, 0.1)
    bar = last_line()
    check(role, "Alt+↑ aumenta a opacidade e a barra mostra o valor", ok and "80%" in bar, f"{stats()['overlayOpacity']}%, alfa {veil_alpha()}, {bar.strip()[:40]}")
    press("alt+Down", 20)
    ok = wait(lambda: stats()["overlayOpacity"] == 10 and veil_alpha() == 25, 3, 0.1)
    check(role, "Alt+↓ para em 10%", ok, f"{stats()['overlayOpacity']}%, alfa {veil_alpha()}")
    time.sleep(0.4)
    grab("opacity-10.png")
    press("alt+Up", 20)
    ok = wait(lambda: stats()["overlayOpacity"] == 100 and veil_alpha() == 255, 3, 0.1)
    check(role, "Alt+↑ para em 100%", ok, f"{stats()['overlayOpacity']}%, alfa {veil_alpha()}")
    time.sleep(0.4)
    grab("opacity-100.png")
    press("alt+Down", 6)
    wait(lambda: stats()["overlayOpacity"] == 70, 3, 0.1)
    press("alt+Left", 3)
    ok = wait(lambda: stats()["overlay"] == chat_cols + 6 and stats()["hole"] == s7["hole"], 3, 0.05)
    check(role, "Alt+← alarga o chat sem encolher o vídeo", ok, f"chat {stats()['overlay']}, {stats()['hole']}")
    press("alt+Right", 3)
    wait(lambda: stats()["overlay"] == chat_cols, 3, 0.05)
    t("key", "ctrl+l")
    ok = wait(lambda: stats()["overlay"] == 0, 3, 0.05)
    check(role, "Ctrl+L esconde o chat sobreposto", ok, str(stats()["hole"]))
    t("key", "ctrl+l")
    wait(lambda: stats()["overlay"] == chat_cols, 3, 0.05)
    check(role, "vídeo não parou", stats()["videoFrames"] > f0 + 60, f"+{stats()['videoFrames'] - f0} quadros")
    t("key", "ctrl+o")
    ok = wait(lambda: stats()["overlay"] == 0 and stats()["hole"] == h0, 3, 0.05)
    check(role, "Ctrl+O devolve o chat para o lado", ok, str(stats()["hole"]))
    t("key", "alt+Up")
    ok = wait(lambda: "Ctrl+O" in last_line(), 3, 0.05)
    check(role, "com o chat ao lado, o atalho explica o que falta", ok, last_line().strip()[:50])

    # ---- Trocar os lados
    role = "lados"
    h0 = list(stats()["hole"])
    chat0 = full_cols - h0[0] + 1 - h0[2] - 1
    t("key", "ctrl+p")
    ok = wait(lambda: stats()["hole"] == [chat0 + 2, 1, h0[2], h0[3]], 3, 0.05)

    def swapped():
        first = screen().splitlines()[0]
        return "Chat" in first.split("│")[0] and "Live" in first.rsplit("│", 1)[-1]

    # O retângulo chega pelo socket de layout e o texto pelo PTY: o título pode vir um quadro depois.
    titles = wait(swapped, 3, 0.05)
    first = screen().splitlines()[0]
    check(role, "Ctrl+P põe o chat à esquerda e a lista à direita", ok and titles, first[:34] + " … " + first.rsplit("│", 1)[-1].strip()[:30])
    press("alt+Right", 2)
    ok = wait(lambda: stats()["hole"] == [chat0 + 6, 1, h0[2] - 4, h0[3]], 3, 0.05)
    check(role, "Alt+→ alarga o chat à esquerda", ok, str(stats()["hole"]))
    press("alt+Left", 2)
    press("ctrl+Left", 2)
    ok = wait(lambda: stats()["hole"] == [chat0 + 2, 1, h0[2] - 4, h0[3]], 3, 0.05)
    check(role, "Ctrl+← alarga a lista à direita", ok, str(stats()["hole"]))
    press("ctrl+Right", 2)
    wait(lambda: stats()["hole"][2] == h0[2], 3, 0.05)
    t("key", "ctrl+o")
    ok = wait(lambda: stats()["overlayLeft"] and stats()["hole"][0] == 1 and stats()["overlay"] == chat0, 3, 0.05)
    s8 = stats()
    check(role, "Ctrl+O põe o chat sobre o vídeo, à esquerda", ok, f"{s8['hole']}, chat {s8['overlay']}")
    time.sleep(0.6)
    ox, oy = origin()
    a, b = two_grabs("glassleft")
    mg = motion(a, b, ox + 10, oy + 200, chat0 * s8["cellW"] - 20, s8["hole"][3] * s8["cellH"] - 400)
    check(role, "o vídeo aparece através do chat à esquerda, em pixels", mg > 0.001, f"{mg:.4f}")
    # Chat mais largo que metade do vídeo: o texto além do meio não pode sumir.
    press("alt+Right", 22)
    wait(lambda: stats()["overlay"] > stats()["hole"][2] / 2, 3, 0.05)
    time.sleep(1.5)
    s9 = stats()
    probe = s9["hole"][2] - s9["overlay"] + 4
    seen = []
    for _ in range(10):
        seen += [int(t("pixel", str(probe), str(y)).split()[3]) for y in range(3, s9["hole"][3] - 3)]
        if max(seen) > 0:
            break
        time.sleep(0.5)
    check(role, "chat largo à esquerda mantém o texto em toda a coluna", max(seen) >= 150, f"chat {s9['overlay']} de {s9['hole'][2]}, coluna {probe}, alfa máx {max(seen)}")
    press("alt+Left", 22)
    wait(lambda: stats()["overlay"] == chat0, 3, 0.05)
    t("key", "ctrl+o")
    time.sleep(0.3)
    t("key", "ctrl+p")
    ok = wait(lambda: stats()["hole"] == h0 and not stats()["overlayLeft"], 3, 0.05)
    check(role, "tudo volta ao lugar", ok, str(stats()["hole"]))

    # ---- Ajuda sobre o vídeo
    role = "ajuda"
    text("?")
    ok = wait(lambda: stats()["holeOn"] and stats()["overlay"] == stats()["hole"][2], 3, 0.1)
    time.sleep(0.5)
    check(role, "abre sobre o vídeo, sem apagá-lo", ok and "===" in screen(), str(stats()["hole"]))
    L, V, R = regions()
    a, b = two_grabs("help")
    mv = motion(a, b, *V)
    check(role, "o vídeo segue visível atrás da ajuda, em pixels", mv > 0.002, f"centro {mv:.4f}")
    shutil.copyfile("m-help-b.png", "help.png")
    hc = stats()["hole"][0] - 1
    alphas = [int(t("pixel", str(hc), str(y)).split()[3]) for y in range(stats()["hole"][3])]
    check(role, "a ajuda usa a opacidade do chat", 178 in alphas and 0 in alphas, f"alfas {sorted(set(alphas))}")
    text("?")
    on = wait(lambda: stats()["holeOn"] and stats()["overlay"] == 0, 3, 0.1)
    time.sleep(0.5)
    a, b = two_grabs("afterhelp")
    L, V, R = regions()
    mv = motion(a, b, *V)
    check(role, "o vídeo volta ao fechar", on and mv > 0.002, f"centro {mv:.4f}")

    t("key", "F1")
    opened = wait(lambda: stats()["holeOn"] and stats()["overlay"] == stats()["hole"][2] and "===" in screen(), 3, 0.1)
    check(role, "F1 abre a ajuda na janela", opened)
    t("key", "F1")
    closed = wait(lambda: stats()["holeOn"] and stats()["overlay"] == 0 and "===" not in screen(), 3, 0.1)
    check(role, "F1 fecha a ajuda na janela", closed)

    # ---- Zapeador: trocar de canal várias vezes sem vazar nada
    role = "zapeador"
    rss0 = rss_mb(st["pid"])
    g0 = stats()["goroutines"]
    fds0 = len(os.listdir(f"/proc/{st['pid']}/fd"))
    times = []
    for i in range(1, 6):
        t("key", "Down")
        time.sleep(0.1)
        s0 = stats()["sessions"]
        a = time.monotonic()
        t("key", "Return")
        got = wait(lambda: stats()["active"] and stats()["sessions"] > s0 and stats()["ttffMs"] > 0 and stats()["videoFrames"] > 0, 40, 0.1)
        time.sleep(2.5)
        times.append(round(time.monotonic() - a - 2.5, 1) if got else None)
    check(role, "5 trocas abriram vídeo", all(times), str(times))
    check(role, "troca média < 10 s", all(times) and sum(times) / len(times) < 10, f"{sum(x for x in times if x) / max(1, len([x for x in times if x])):.1f}s")
    time.sleep(2)
    n_sl = len(own(st["pid"], "streamlink"))
    n_shim = len(own(st["pid"], "mpv"))
    check(role, "um streamlink e um player por vez", n_sl == 1 and n_shim == 1, f"streamlink={n_sl} shim={n_shim}")
    rss1, g1, fds1 = rss_mb(st["pid"]), stats()["goroutines"], len(os.listdir(f"/proc/{st['pid']}/fd"))
    check(role, "sem vazamento de goroutine ou fd após 5 trocas", g1 <= g0 + 2 and fds1 <= fds0 + 6, f"gor {g0}->{g1}, fds {fds0}->{fds1}")
    print(f"INFO [{role}] RSS {rss0}->{rss1} MB", flush=True)
    check(role, "ainda uma janela só", len(ours()) == 1 and not real_mpv_windows(), f"{len(ours())}")

    # ---- Usuário de tiling: a janela muda de tamanho
    role = "tiling"
    c0, w0 = stats()["cols"], stats()["W"]
    declare_rule(FOOT_CLASS, FOOT_CLASS)
    subprocess.Popen(["setsid", "-f", "foot", f"--app-id={FOOT_CLASS}", "sh", "-c", "exec sleep 8"], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    split = wait(lambda: stats()["W"] < w0 - 200, 5, 0.1)
    c1, w1 = stats()["cols"], stats()["W"]
    f0 = stats()["videoFrames"]
    t_rect = time.monotonic()
    # O retângulo antigo (92 colunas) também é menor que 100: só vale o que cabe na janela nova.
    converged = wait(lambda: (lambda s: 0 < s["hole"][2] < 100 and s["hole"][0] < 40 and s["hole"][0] - 1 + s["hole"][2] <= s["cols"])(stats()), 3, 0.02)
    rect_ms = (time.monotonic() - t_rect) * 1000
    s5 = stats()
    if c1 < 80:
        wait(lambda: "chat hidden" in last_line(), 3, 0.05)  # a TUI redesenha depois do SIGWINCH
        bar = last_line()
        notice_rows = [l for l in screen().splitlines() if "widen the terminal" in l or "Chat hidden (" in l]
        check(role, "janela estreita avisa em uma linha, na barra", "chat hidden" in bar and not notice_rows, bar.strip()[-60:])
    check(role, "tile lado a lado reajusta as três colunas", split and c1 < c0 and converged, f"W {w0}->{w1}, cols {c0}->{c1}, vídeo {s5['hole']}, convergiu em {rect_ms:.0f} ms")
    time.sleep(1.5)
    check(role, "vídeo continua depois do resize", stats()["videoFrames"] > f0 + 30, f"+{stats()['videoFrames'] - f0} quadros")
    back = wait(lambda: stats()["W"] >= w0 - 4, 10, 0.2)
    check(role, "volta ao tamanho original", back and stats()["cols"] == c0, f"cols {stats()['cols']}")
    grab("flows-tiling.png")

    # ---- Falha: o player morre no meio
    role = "falha"
    shim = own(st["pid"], "mpv")
    if shim:
        os.kill(int(shim[0]), signal.SIGKILL)
    ended = wait(lambda: not stats()["active"], 8, 0.2)
    check(role, "player morto encerra a sessão embutida", ended)
    time.sleep(1.5)
    last = last_line()
    check(role, "app segue vivo e o TUI informa a falha", "failed" in last.lower(), last.strip()[:70])
    v_before = stats()["videoFrames"]
    text("r")
    again = wait(lambda: stats()["active"] and stats()["videoFrames"] > v_before + 5, 35, 0.2)
    check(role, "r reabre o player", again, f"sessões {stats()['sessions']}")

    # ---- Parar o vídeo: volta a TUI inteira sem quadro de vídeo forçando a tela
    role = "parar"
    wait(lambda: stats()["videoFrames"] > 0, 5)
    t("key", "ctrl+x")
    stopped = wait(lambda: not stats()["active"], 8, 0.1)
    check(role, "Ctrl+X encerra o vídeo e o app segue", stopped)
    time.sleep(1.5)
    dv = pixels("stop")
    check(role, "TUI redesenhada em pixels na janela inteira", dv > 0.03, f"desvio {dv:.4f}, cols {stats()['cols']}")
    t("key", "Return")
    check(role, "abre outro canal depois de parar", wait(lambda: stats()["active"] and stats()["videoFrames"] > 5, 30, 0.2))

    # ---- Saída: fechar a janela com vídeo tocando
    role = "saida"
    descendants = [p for p in children(st["pid"]) if p != st["pid"]]
    t("quit")
    gone = wait(exited, 8, 0.1)
    time.sleep(1.5)
    check(role, "fechar a janela encerra o app", gone)
    left = [f"{p}:{comm(p)}" for p in descendants if comm(p)]
    check(role, "sem processo órfão", not left, str(left))
    check(role, "sem janela restante", not ours(), str(len(ours())))
    check(role, "sem temporário restante", not glob.glob(f"{TMP}/ttv-shell-*"), str(glob.glob(f"{TMP}/ttv-shell-*"))[:80])

    # ---- A largura sobrevive a fechar e abrir
    role = "memoria"
    up()
    open_channel(0)
    wait(lambda: stats()["active"], 10, 0.2)
    time.sleep(1)
    base = stats()["hole"][0] - 2
    press("ctrl+Right", 5)
    time.sleep(1.2)  # a gravação espera a tecla parar
    down()
    cfg = open("cfg/ttv-tui/config.toml").read()
    check(role, "largura gravada no config", f"list_columns = {base + 10}" in cfg, f"list_columns = {base + 10}")
    declare_rule()
    launch()  # sem apagar o config
    guard_monitor()
    ok = wait(lambda: stats()["hole"][0] == base + 12, 8, 0.1)
    check(role, "abre com a largura salva", ok, str(stats()["hole"]))
    down()


def preflight():
    """Tudo o que a bateria exige, antes de compilar ou abrir qualquer coisa."""
    missing = []
    if sys.platform != "linux":
        missing.append("Linux (a janela só existe nele)")
    if not os.environ.get("HYPRLAND_INSTANCE_SIGNATURE") or not os.environ.get("WAYLAND_DISPLAY"):
        missing.append("sessão do Hyprland (HYPRLAND_INSTANCE_SIGNATURE e WAYLAND_DISPLAY vazios)")
    tools = {
        "hyprctl": "Hyprland", "grim": "grim", "magick": "ImageMagick", "foot": "foot", "streamlink": "streamlink",
        "go": "Go", "gcc": "gcc (cgo)", "pkg-config": "pkgconf", "pgrep": "procps", "setsid": "util-linux",
    }
    for exe, pkg in tools.items():
        if not shutil.which(exe):
            missing.append(f"{exe} ({pkg})")
    if not glob.glob("/dev/dri/renderD*"):
        missing.append("GPU para VA-API (nenhum /dev/dri/renderD*)")
    if missing:
        raise Missing("; ".join(missing))
    try:
        monitors()
    except (OSError, ValueError) as e:
        raise Missing(f"hyprctl não fala com o compositor: {e}")
    if subprocess.run(["pkg-config", "--exists", "sdl3", "mpv"]).returncode != 0:
        raise Missing("bibliotecas de desenvolvimento do SDL3 e do libmpv (pkg-config sdl3 mpv)")
    for host in ("gql.twitch.tv", "usher.ttvnw.net"):
        try:
            socket.create_connection((host, 443), 6).close()
        except OSError as e:
            raise Missing(f"rede: {host}:443 não responde ({e})")
    if any(m["name"] == MON for m in monitors()):
        raise Missing(f"o monitor {MON} já existe: há outra execução em curso, ou sobrou de uma interrompida (hyprctl output remove {MON})")


def build():
    os.makedirs(f"{RUN}/bin")
    for name, cwd, pkg in (("ttv-tui", REPO, "./cmd/ttv-tui"), ("ttv-tui-window", f"{REPO}/window", ".")):
        r = subprocess.run(["go", "build", "-buildvcs=false", "-o", f"{RUN}/bin/{name}", pkg], cwd=cwd, capture_output=True, text=True)
        if r.returncode != 0:
            raise BuildFailed(f"{name}:\n{r.stdout}{r.stderr}")
    if subprocess.run([f"{RUN}/bin/ttv-tui-window", "--probe"], capture_output=True).returncode != 0:
        raise Missing("ttv-tui-window --probe falhou: sem display Wayland ou sem fonte utilizável")


def wait_load(limit, secs):
    start, end = load1(), time.monotonic() + secs
    while load1() >= limit and time.monotonic() < end:
        time.sleep(10)
    waited = secs - max(0, end - time.monotonic())
    note = "" if load1() < limit else f" (não baixou de {limit:g}: fps e latência podem falhar por carga)"
    print(f"INFO [carga] load de 1 min: {load1():.2f}; era {start:.2f}, esperou {waited:.0f} s{note}", flush=True)


def cleanup():
    """Desfaz só o que a bateria criou: processos marcados, regras e o monitor virtual."""
    for s in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP, signal.SIGALRM):
        signal.signal(s, signal.SIG_IGN)
    notes = []
    if PROC is not None and PROC.poll() is None:
        try:
            t("quit")
        except OSError:
            pass
        wait(exited, 6)
    for sig in (signal.SIGTERM, signal.SIGKILL):
        left = marked()
        if not left:
            break
        notes.append(f"{signal.Signals(sig).name} em {sorted(f'{p}:{comm(p)}' for p in left)}")
        for p in left:
            try:
                os.kill(p, sig)
            except OSError:
                pass
        wait(lambda: not marked(), 4)
    if PROC is not None:
        try:
            PROC.wait(2)
        except subprocess.TimeoutExpired:
            pass
    rules_off = True
    if made_rules:
        r = subprocess.run(["hyprctl", "eval", "for _, r in pairs(_G.ttvflows or {}) do r:set_enabled(false) end; _G.ttvflows = nil"], capture_output=True, text=True)
        rules_off = r.stdout.strip() == "ok"
        if not rules_off:
            notes.append(f"hyprctl recusou desativar as regras: {r.stdout.strip()[:120]}")
    if made_monitor:
        subprocess.run(["hyprctl", "output", "remove", MON], capture_output=True)
        wait(lambda: not any(m["name"] == MON for m in monitors()), 5)
    if TMP:
        shutil.rmtree(TMP, ignore_errors=True)  # inclui o socket de teste e o que a janela não apagou ao ser morta
    shutil.rmtree(f"{RUN}/bin", ignore_errors=True)  # o binário se recompila; a evidência fica
    shutil.rmtree(f"{RUN}/cache", ignore_errors=True)
    try:
        mon_left = [m["name"] for m in monitors() if m["name"] == MON]
        win_left = [c["class"] for c in clients() if c["class"].startswith(CLASS)]
    except (OSError, ValueError):
        mon_left, win_left = ["?"], ["?"]
    proc_left = [f"{p}:{comm(p)}" for p in marked()]
    tmp_left = bool(TMP) and os.path.exists(TMP)
    clean = not (made_monitor and mon_left) and not win_left and not proc_left and rules_off and not tmp_left
    print(f"LIMPEZA: {'ok' if clean else 'INCOMPLETA'}; monitor {MON}: {mon_left or 'removido'}; janelas de teste: {win_left or 'nenhuma'}; "
          f"processos da execução: {proc_left or 'nenhum'}; temporários: {TMP if tmp_left else 'removidos'}{'; ' + '; '.join(notes) if notes else ''}", flush=True)
    return clean


def on_signal(num, _frame):
    raise Invalid("tempo esgotado" if num == signal.SIGALRM else f"interrompido por {signal.Signals(num).name}")


def take_lock():
    """Uma execução por vez: o monitor, as regras e as medições são da máquina inteira."""
    global lock_file
    lock_file = open(os.path.join(os.environ.get("XDG_RUNTIME_DIR") or tempfile.gettempdir(), "ttv-tui-window-flows.lock"), "w")
    try:
        fcntl.flock(lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        raise Missing("outra execução da bateria está em curso")


def main():
    global RUN, TMP, TEST, made_monitor
    ap = argparse.ArgumentParser(description="Bateria de ponta a ponta da janela única (Hyprland, live real).")
    ap.add_argument("--max-load", type=float, default=6, help="espera o load de 1 min ficar abaixo disto antes de medir (padrão 6)")
    ap.add_argument("--load-wait", type=int, default=900, help="quanto esperar pela carga, em segundos (padrão 900; 0 não espera)")
    ap.add_argument("--timeout", type=int, default=900, help="limite dos fluxos, em segundos (padrão 900)")
    ap.add_argument("--out", default=os.environ.get("TTV_FLOWS_DIR") or os.path.expanduser("~/.cache/scratch/ttv-tui-window-flows"),
                    help="onde criar o diretório da execução (padrão ~/.cache/scratch/ttv-tui-window-flows ou $TTV_FLOWS_DIR)")
    args = ap.parse_args()

    code = 1
    try:
        take_lock()
        preflight()
        RUN = os.path.join(os.path.abspath(args.out), time.strftime("%Y%m%d-%H%M%S") + f"-{os.getpid()}")
        os.makedirs(RUN)
        os.chdir(RUN)
        os.environ[MARK] = os.path.basename(RUN)
        # Curto de propósito: sun_path tem 108 bytes e os sockets da janela ficam aqui.
        TMP = tempfile.mkdtemp(prefix="ttvflows-", dir=os.environ.get("XDG_RUNTIME_DIR") or None)
        TEST = f"{TMP}/test.sock"
        sys.stdout = Tee(sys.__stdout__, open("flows.log", "w"))
        print(f"INFO [exec] diretório: {RUN}", flush=True)
        build()
        commit = subprocess.run(["git", "-C", REPO, "describe", "--always", "--dirty"], capture_output=True, text=True).stdout.strip()
        print(f"INFO [exec] compilado de {commit or 'checkout sem Git'}", flush=True)
        if args.load_wait > 0:
            wait_load(args.max_load, args.load_wait)
        load_start = load1()

        BASELINE.update(l.split(" ", 1)[0] for l in sh("pgrep -a ttv-tui; pgrep -a streamlink; pgrep -a mpv").splitlines())
        MPV_BASE.update(c["address"] for c in clients() if "mpv" in c["class"].lower())
        print(f"INFO [base] {len(BASELINE)} processos e temporários já existentes ignorados", flush=True)

        for s in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP, signal.SIGALRM):
            signal.signal(s, on_signal)
        signal.alarm(args.timeout)
        if any(m["name"] == MON for m in monitors()):  # a compilação e a espera de carga levam minutos
            raise Missing(f"o monitor {MON} apareceu durante a preparação (hyprctl output remove {MON})")
        made_monitor = True  # antes de criar: um sinal logo depois ainda precisa removê-lo
        r = subprocess.run(["hyprctl", "output", "create", "headless", MON], capture_output=True, text=True)
        if not wait(lambda: any(m["name"] == MON for m in monitors()), 5, 0.1):
            raise Invalid(f"o Hyprland não criou o monitor virtual: {r.stdout.strip()[:200]}")
        m = test_monitor()
        same = (m["width"], m["height"]) == (CAL_W, CAL_H)
        print(f"INFO [exec] monitor {MON}: {m['width']}x{m['height']}, escala {m['scale']:g}, fonte de {CAL_PT * CAL_SCALE / m['scale']:.2f} pt"
              + ("" if same else f" (os critérios foram calibrados em {CAL_W}x{CAL_H})"), flush=True)

        try:
            flows()
        except Abort as e:
            print(f"ABORTADO: {e}", flush=True)
        except Invalid:
            raise
        except Exception:
            alive = "viva" if not exited() else f"morta (código {PROC.returncode})" if PROC else "nunca abriu"
            check("exec", "os fluxos rodaram até o fim", False, f"janela {alive}; {traceback.format_exc().strip().splitlines()[-1]}")
            traceback.print_exc(file=sys.stdout)
        signal.alarm(0)

        print()
        passed = sum(1 for r in results if r[2])
        print(f"RESUMO: {passed}/{len(results)} verificações passaram; load de 1 min {load_start:.2f} no início e {load1():.2f} no fim")
        for r in results:
            if not r[2]:
                print(f"  falhou: [{r[0]}] {r[1]} :: {r[3]}")
        code = 0 if results and passed == len(results) else 1
    except Missing as e:
        print(f"FALTA PRÉ-REQUISITO, nada foi testado: {e}", flush=True)
        code = 2
    except Invalid as e:
        print(f"INVÁLIDO, a rodada não vale como evidência: {e} ({sum(1 for r in results if r[2])}/{len(results)} verificações até aqui)", flush=True)
        code = 3
    except BuildFailed as e:
        print(f"NÃO COMPILOU: {e}", flush=True)
        code = 4
    finally:
        if RUN:
            if not cleanup() and code == 0:
                code = 3
            print(f"Evidências (capturas, flows.log, window.out): {RUN}", flush=True)
    return code


if __name__ == "__main__":
    sys.exit(main())
