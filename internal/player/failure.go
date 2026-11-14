package player

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// Failure classifies why playback could not start or stopped with an error.
// The UI turns each class into a localized message with the next step; the
// original streamlink/mpv line stays in Event.LastLine as the technical detail.
type Failure string

const (
	// FailureNone means no classified failure (normal states, or no evidence).
	FailureNone Failure = ""
	// FailureOffline: streamlink found no playable stream (channel offline or
	// inexistent). Retrying right away is pointless; pick another channel.
	FailureOffline Failure = "offline"
	// FailureNetwork: connection refused, DNS, proxy, TLS or unreachable network.
	FailureNetwork Failure = "network"
	// FailureTimeout: a connection or read timed out, or mpv never opened its
	// IPC socket within SocketWait.
	FailureTimeout Failure = "timeout"
	// FailureStreamlinkMissing: the streamlink executable is not installed.
	FailureStreamlinkMissing Failure = "streamlink_missing"
	// FailureMPVMissing: the mpv executable is not installed.
	FailureMPVMissing Failure = "mpv_missing"
	// FailureNoPlugin: streamlink has no plugin capable of handling the URL.
	FailureNoPlugin Failure = "no_plugin"
	// FailureBlocked: the site answered HTTP 403 to streamlink, as an anti-bot
	// wall does. The connection worked; the request was refused.
	FailureBlocked Failure = "blocked"
	// FailureBrowserMissing: the site answered HTTP 403 and streamlink could
	// not start the Chromium-based browser that solves the challenge: none was
	// found, or the configured path is not an executable. The Kick plugin of
	// streamlink 8.x does this when its API refuses the request.
	FailureBrowserMissing Failure = "browser_missing"
	// FailureVOD: streamlink refused an on-demand video that its plugin does
	// not play (YouTube VOD: "This plugin does not support VOD content").
	FailureVOD Failure = "vod"
	// FailureBotCheck: the site presented a bot verification challenge
	// (YouTube LOGIN_REQUIRED: "Sign in to confirm you're not a bot").
	FailureBotCheck Failure = "bot_check"
	// FailureLoginRequired: the stream requires a platform account sign-in
	// (age-restricted, members-only, or private) rather than a bot check.
	FailureLoginRequired Failure = "login_required"
	// FailureStreamlinkBroken: this streamlink build failed on its own, not
	// because of the channel or the network: a plugin or Python runtime error,
	// a binary that cannot run, or an exit with an error status before any
	// stream reached the player. It is the only class that counts against a
	// freshly updated managed streamlink (see BlamesStreamlink).
	FailureStreamlinkBroken Failure = "streamlink_broken"
	// FailureUnknown: an error without a recognized cause.
	FailureUnknown Failure = "unknown"
)

// BlamesStreamlink reports whether f is evidence that the streamlink build in
// use is broken. Offline channels, network and timeout failures, a URL no plugin
// handles, a request the site refused, missing tools and unclassified errors say
// nothing about the build, so
// they never count: an update must not be undone because the channel is offline
// or the network is down.
func (f Failure) BlamesStreamlink() bool {
	return f == FailureStreamlinkBroken
}

// MissingToolError reports a required executable that is not on PATH.
type MissingToolError struct {
	Tool string
	Err  error
}

func (e *MissingToolError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("player: %s not found: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("player: %s not found", e.Tool)
}

func (e *MissingToolError) Unwrap() error { return e.Err }

// failureForTool maps an executable name to its missing-tool class.
func failureForTool(tool string) Failure {
	base := filepath.Base(tool)
	if i := strings.LastIndex(base, `\`); i >= 0 {
		base = base[i+1:]
	}
	name := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(base), ".exe"), ".com")
	if name == "mpv" {
		return FailureMPVMissing
	}
	return FailureStreamlinkMissing
}

// ExecError reports that the process of a session could not be started: the
// executable exists but the system refused to run it, or it is not there.
type ExecError struct {
	Err error
}

func (e *ExecError) Error() string { return "player: start: " + e.Err.Error() }

func (e *ExecError) Unwrap() error { return e.Err }

// FailureOf classifies an error returned by Manager.Start.
//
// O texto do erro não é classificado: um erro de validação repete a URL alvo,
// que vem de terceiros (o nome do canal, o link colado). Só o tipo conta, e só
// um executável que o sistema recusa como formato inválido culpa o streamlink.
func FailureOf(err error) Failure {
	if err == nil {
		return FailureNone
	}
	var missing *MissingToolError
	if errors.As(err, &missing) {
		return failureForTool(missing.Tool)
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return failureForTool(execErr.Name)
	}
	var startErr *ExecError
	if errors.As(err, &startErr) && errors.Is(startErr.Err, syscall.ENOEXEC) {
		return FailureStreamlinkBroken
	}
	return FailureUnknown
}

// ClassifyLine maps one streamlink/mpv output line to a failure class, or
// FailureNone when the line proves nothing. Timeouts are checked before network
// errors because urllib3 reports a connect timeout inside a generic
// "Max retries exceeded" connection error.
//
// Linhas de nível info ou debug do streamlink ("[cli][info] Found matching
// plugin twitch for URL ...") não classificam: repetem a URL e o nome do canal,
// texto de terceiros que pode conter "keyerror" ou "offline". Nomes de exceção
// Python só contam no formato "<Nome>Error:" que o Python imprime.
func ClassifyLine(line string) Failure {
	return classifyLine(line, false)
}

// ClassifyTargetLine is ClassifyLine for a line of the session of t: the target
// URL and the session identity become a marker before the line is classified.
func ClassifyTargetLine(line string, t Target) Failure {
	return classifyLine(maskTarget(line, t), false)
}

// classifyLine is ClassifyLine; afterTraceback accepts a bare exception name
// ("KeyError" with no message), which only means something as the last line of
// a Python traceback.
func classifyLine(line string, afterTraceback bool) Failure {
	if isQuietLogLine(line) {
		return FailureNone
	}
	l := strings.ToLower(line)
	// A 404 or 403 on one segment or on one playlist reload happens during
	// normal playback (a prefetch segment that expired, a reload racing the
	// end of an ad) and streamlink keeps going. Only the same status on the
	// channel or its playlist says something about the channel.
	perSegment := isPerSegmentLine(l)
	switch {
	case l == "":
		return FailureNone
	case isBrowserMissingLine(line):
		// First: the "Invalid web browser executable" line repeats the
		// configured path, which may contain any of the phrases below.
		return FailureBrowserMissing
	case isYouTubeBotCheckLine(l):
		return FailureBotCheck
	case isYouTubeLoginRequiredLine(l):
		return FailureLoginRequired
	case strings.Contains(l, "this plugin does not support vod content"):
		return FailureVOD
	case strings.Contains(l, "no playable streams found"),
		strings.Contains(l, "this stream is offline"),
		strings.Contains(l, "channel is offline"),
		strings.Contains(l, "[plugins.youtube][error]") && strings.Contains(l, "live_stream_offline"),
		// The site answered that the channel or its playlist does not exist: a
		// channel the API still lists as live right after it ended, or a wrong
		// name. The connection worked, so this is not a network error.
		!perSegment && strings.Contains(l, "404 client error"):
		return FailureOffline
	case !perSegment && strings.Contains(l, "403 client error"):
		// Before the network cases: streamlink reports it inside "Unable to open URL".
		return FailureBlocked
	case strings.Contains(l, "failed to start player") && strings.Contains(l, "mpv"),
		strings.Contains(l, "mpv: command not found"),
		strings.Contains(l, "mpv: not found"):
		return FailureMPVMissing
	case strings.Contains(l, "streamlink: command not found"),
		strings.Contains(l, "streamlink: not found"):
		return FailureStreamlinkMissing
	case strings.Contains(l, "no plugin can handle url"):
		return FailureNoPlugin
	case strings.Contains(l, "timed out"),
		strings.Contains(l, "timeouterror"),
		strings.Contains(l, "timeout"):
		return FailureTimeout
	case strings.Contains(l, "unable to open url"),
		strings.Contains(l, "connectionerror"),
		strings.Contains(l, "connection refused"),
		strings.Contains(l, "connection reset"),
		strings.Contains(l, "failed to establish a new connection"),
		strings.Contains(l, "max retries exceeded"),
		strings.Contains(l, "name or service not known"),
		strings.Contains(l, "temporary failure in name resolution"),
		strings.Contains(l, "failed to resolve"),
		strings.Contains(l, "network is unreachable"),
		strings.Contains(l, "proxyerror"),
		strings.Contains(l, "sslerror"):
		return FailureNetwork
	case strings.Contains(l, "plugin error"),
		strings.Contains(l, "unable to validate"),
		pythonException.MatchString(line),
		afterTraceback && bareException.MatchString(line),
		strings.Contains(l, "fatal python error"),
		strings.Contains(l, "error while loading shared libraries"),
		strings.Contains(l, "exec format error"):
		// Last on purpose: a plugin failure caused by the network or an
		// offline channel is reported as such by the cases above.
		return FailureStreamlinkBroken
	default:
		return FailureNone
	}
}

// isBrowserMissingLine reports the error streamlink logs when the challenge of
// a 403 needs a web browser it cannot start (webbrowser/webbrowser.py in
// streamlink 8.6, logged by plugins/kick.py):
//
//	[plugins.kick][error] Could not find Chromium-based web browser executable: Please set the path to a supported web browser using --webbrowser-executable
//	[plugins.kick][error] Invalid web browser executable: /path/from/the/config
//
// Só a linha inteira vale, com o logger e o nível do plugin kick, como em
// detect.go: o mesmo texto dentro de outra linha (o caminho do player numa
// falha do mpv, a mensagem de outro logger) é texto de terceiros. Da segunda
// linha, só o prefixo é fixo; o resto é o caminho do config.
func isBrowserMissingLine(line string) bool {
	l := strings.TrimRight(line, " \r")
	return l == browserNotFoundLine ||
		(strings.HasPrefix(l, browserInvalidPrefix) && len(l) > len(browserInvalidPrefix))
}

const (
	browserNotFoundLine  = "[plugins.kick][error] Could not find Chromium-based web browser executable: Please set the path to a supported web browser using --webbrowser-executable"
	browserInvalidPrefix = "[plugins.kick][error] Invalid web browser executable: "
)

func isYouTubeBotCheckLine(l string) bool {
	if !strings.Contains(l, "[plugins.youtube][error]") {
		return false
	}
	return strings.Contains(l, "sign in to confirm you're not a bot") ||
		strings.Contains(l, "sign in to confirm you\u2019re not a bot")
}

func isYouTubeLoginRequiredLine(l string) bool {
	return strings.Contains(l, "[plugins.youtube][error]") &&
		strings.Contains(l, "login_required")
}

// buildExceptions são as exceções Python que indicam defeito do próprio
// streamlink (plugin, import, código), e não do canal ou da rede.
const buildExceptions = `(?:PluginError|ModuleNotFoundError|ImportError|SyntaxError|AttributeError|TypeError|KeyError)`

// pythonException casa o nome da exceção como o Python o imprime, com
// maiúsculas e dois-pontos: "KeyError: 'data'", "streamlink.exceptions.PluginError: ...".
// Um login como "keyerror" ou "xtypeerrorx" não tem esse formato.
var pythonException = regexp.MustCompile(buildExceptions + `:`)

// bareException é a última linha de um traceback cuja exceção não tem mensagem.
var bareException = regexp.MustCompile(`^\s*(?:[A-Za-z_]\w*\.)*` + buildExceptions + `\s*$`)

// quietLogLine casa o prefixo de log do streamlink nos níveis que não relatam
// erro ("[cli][info] ", "[plugins.twitch][debug] ").
var quietLogLine = regexp.MustCompile(`^\[[^\]]*\]\[(?:info|debug|trace|all)\] `)

// isQuietLogLine reports a streamlink log line below the warning level.
func isQuietLogLine(line string) bool {
	return quietLogLine.MatchString(line)
}

// isTracebackStart reports the first line of a Python traceback.
func isTracebackStart(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "Traceback (most recent call last):")
}

// targetMarker substitui a URL alvo e a identidade da sessão antes da
// classificação. Não contém nenhuma das frases que o classificador procura.
const targetMarker = "<alvo>"

// maskTarget troca, em line, a URL de t e a identidade da sessão por
// targetMarker, sem diferenciar maiúsculas.
//
// A URL é trocada onde aparecer: não tem espaço (checkStreamURL recusa), então
// nunca cobre uma frase do classificador. A identidade (o login, "kick:<slug>")
// e o que vem depois do seu primeiro ":" só são trocados quando vêm depois de
// "/", "=", aspas ou apóstrofo, como num caminho, numa query ou num repr do
// Python: um login "streams" solto não pode apagar "No playable streams found".
func maskTarget(line string, t Target) string {
	if t.URL != "" {
		line = replaceFold(line, t.URL, func(string, int, int) bool { return true })
	}
	names := []string{t.Label}
	if i := strings.IndexByte(t.Label, ':'); i >= 0 {
		names = append(names, t.Label[i+1:])
	}
	for _, name := range names {
		if name == "" || name == t.URL {
			continue
		}
		line = replaceFold(line, name, delimitedName)
	}
	return line
}

// delimitedName aceita a ocorrência de line[start:end] precedida por um
// separador de caminho, de query ou de string, e seguida de um caractere que
// não continua um nome.
func delimitedName(line string, start, end int) bool {
	if start == 0 || !strings.ContainsRune(`/='"`, rune(line[start-1])) {
		return false
	}
	return end == len(line) || !isNameByte(line[end])
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// replaceFold troca por targetMarker as ocorrências de old em s, sem
// diferenciar maiúsculas ASCII; accept decide quais ocorrências valem.
func replaceFold(s, old string, accept func(s string, start, end int) bool) string {
	ls, lo := lowerASCII(s), lowerASCII(old)
	var b strings.Builder
	last := 0
	for i := 0; i <= len(ls)-len(lo); {
		j := strings.Index(ls[i:], lo)
		if j < 0 {
			break
		}
		start, end := i+j, i+j+len(lo)
		if accept(s, start, end) {
			b.WriteString(s[last:start])
			b.WriteString(targetMarker)
			last = end
			i = end
			continue
		}
		i = start + 1
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// lowerASCII preserva o comprimento em bytes, ao contrário de strings.ToLower.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// isPerSegmentLine reports the streamlink HLS lines about a single segment,
// its map or one playlist reload, which the stream survives
// (stream/hls/hls.py in streamlink 8.6).
func isPerSegmentLine(l string) bool {
	return strings.Contains(l, "failed to fetch segment") ||
		strings.Contains(l, "failed to fetch map for segment") ||
		strings.Contains(l, "download of segment") ||
		strings.Contains(l, "reloading failed")
}

// isFatalLine reports lines after which streamlink cannot recover: playback
// will not start, so the session ends right away instead of waiting for the
// process or the IPC socket deadline.
func isFatalLine(f Failure) bool {
	switch f {
	case FailureOffline, FailureMPVMissing, FailureStreamlinkMissing, FailureNoPlugin, FailureVOD, FailureBotCheck, FailureLoginRequired:
		return true
	default:
		return false
	}
}

// classifyTerminal picks the failure for an error transition: the line-based
// class when one is recognized, otherwise fallback. A URL e a identidade de t
// viram marcador antes, como em ClassifyTargetLine.
func classifyTerminal(line string, t Target, fallback Failure) Failure {
	if f := ClassifyTargetLine(line, t); f != FailureNone {
		return f
	}
	if fallback != FailureNone {
		return fallback
	}
	return FailureUnknown
}
