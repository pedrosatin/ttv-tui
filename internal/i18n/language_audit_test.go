package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// identicalInBothLanguages lists every key whose pt-BR value is deliberately
// the same as the English one, with the reason. A new key with equal values
// must be added here on purpose (and an entry that stops being identical must
// leave), so an English string pasted into pt-BR cannot pass unnoticed.
var identicalInBothLanguages = map[string]string{
	// Proper name.
	"app.title": "nome do programa",
	// Symbols and formats with no words.
	"categories.viewers_unknown":    "símbolo — (sem dado)",
	"channels.viewers_unknown":      "símbolo — (sem dado)",
	"channels.title_unknown":        "símbolo — (sem dado)",
	"channel_filter.language_count": "formato %s (%d)",
	"channel_filter.audience_min":   "formato %d+",
	"chat.usernotice.with_text":     "formato autor: texto",
	"chat.input.counter":            "formato %d/%d",
	"status.channel_ref":            "formato #canal",
	// Units: s, m and h are the same symbols in pt-BR.
	"chat.mod.dur_s":   "unidade s",
	"chat.mod.dur_m":   "unidade m",
	"chat.mod.dur_m_s": "unidades m e s",
	"chat.mod.dur_h":   "unidade h",
	"chat.mod.dur_h_m": "unidades h e m",
	// Cognate abbreviation: "vol" is the usual short form of volume in pt-BR too,
	// and the bar has no room for the whole word at 80 columns.
	"player.vol": "abreviação de volume, igual em pt-BR",
	// Loanwords the whole pt-BR catalog already uses: "Chat", "chat", "offline",
	// "[mod]". Twitch's own pt-BR interface uses the same words.
	"chat.title":           "chat é o termo usado em todo o catálogo pt-BR",
	"help.panel_chat":      "chat é o termo usado em todo o catálogo pt-BR",
	"chat.mod_prefix":      "[mod] abrevia moderador nas duas línguas",
	"channels.offline":     "offline é o termo usado pela Twitch em pt-BR",
	"channels.offline_tag": "offline é o termo usado pela Twitch em pt-BR",
	// The word "layout" is a pt-BR loanword (help.layout and the README use it);
	// the mode that follows it comes from layout.mode.*, translated.
	"layout.mode.chat": "chat é o termo usado em todo o catálogo pt-BR",
	"status.layout":    "layout é empréstimo consolidado; o modo vem de layout.mode.*",
}

// englishOnlyWords are words with no use in pt-BR interface copy. A pt-BR value
// containing one of them (outside quoted commands, flags, key chords and
// placeholders) is English left in the Portuguese catalog. Words that exist in
// both languages ("volume", "use", "pause", "for", "live") are deliberately
// absent: they are valid Portuguese. Loanwords the project keeps (chat, player,
// login, token, cookie, layout, offline, mod) are absent for the same reason.
var englishOnlyWords = wordSet(`
the and or with from into onto not is are was were be been being you your
this that these those it its in of on at by an can cannot will would should has have had
don't doesn't isn't aren't won't can't
to
error errors warning warnings failed fail failure unable found missing invalid expired
connection connect connecting connected disconnected reconnecting loading
message messages channel channels category categories search filter filters
list lists focus split volume-up
mute muted playing played ended retry try again press select selected open close closed quit exit
help save saved send sent
language languages all any none unknown please wait
viewers viewer followers follower subscribers subscriber
slow mode banned ban deleted copy copied draft unlock file path
timeout timed scopes scope config keyring space version settings
username user users
now soon later never always
sign signed
`)

// ptBRAllowedWords exempts a word from englishOnlyWords. It is the explicit
// allowlist: add a word here, with the reason, instead of weakening the list.
var ptBRAllowedWords = map[string]string{}

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

var (
	formatVerb    = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)
	quotedLiteral = regexp.MustCompile(`'[^']*'|"[^"]*"|` + "`[^`]*`")
)

// ptBRWords returns the plain words of a pt-BR value: format verbs, quoted
// commands and any token with a symbol (flags, key chords, identifiers,
// numbers, paths, hosts) and upper-case names are dropped; what is left is Portuguese copy.
func ptBRWords(value string) []string {
	value = formatVerb.ReplaceAllString(value, " ")
	value = quotedLiteral.ReplaceAllString(value, " ")
	var words []string
	var tokens []string
	for _, f := range strings.Fields(value) {
		// "split/chat/list/focus" is four words; "20/30s" and "↑/↓" drop below.
		tokens = append(tokens, strings.Split(f, "/")...)
	}
	for _, tok := range tokens {
		tok = strings.Trim(tok, ".,;:()[]!?¿¡·—|…")
		if tok == "" || strings.ContainsAny(tok, "-+/_:#@%.0123456789") {
			continue
		}
		if len(tok) > 1 && tok == strings.ToUpper(tok) {
			continue // PATH, DISPLAY, OSC: environment names and acronyms
		}
		clean := strings.TrimFunc(tok, func(r rune) bool { return !unicode.IsLetter(r) })
		if clean != "" {
			words = append(words, strings.ToLower(clean))
		}
	}
	return words
}

// auditPTBR returns one line per problem found in the pt-BR catalog.
func auditPTBR(en, pt map[string]string) []string {
	var problems []string
	keys := make([]string, 0, len(pt))
	for k := range pt {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := pt[k]
		if v == "" {
			problems = append(problems, k+": pt-BR value is empty")
			continue
		}
		if en[k] == v {
			if _, ok := identicalInBothLanguages[k]; !ok {
				problems = append(problems, k+": pt-BR value "+quote(v)+" is identical to English; translate it or add the key to identicalInBothLanguages with the reason")
			}
			continue
		}
		for _, w := range ptBRWords(v) {
			if englishOnlyWords[w] && ptBRAllowedWords[w] == "" {
				problems = append(problems, k+": pt-BR value "+quote(v)+" has the English word "+quote(w))
				break
			}
		}
	}
	// The allowlist must stay honest.
	allowed := make([]string, 0, len(identicalInBothLanguages))
	for k := range identicalInBothLanguages {
		allowed = append(allowed, k)
	}
	sort.Strings(allowed)
	for _, k := range allowed {
		switch {
		case pt[k] == "":
			problems = append(problems, k+": listed in identicalInBothLanguages but missing from pt-BR")
		case en[k] != pt[k]:
			problems = append(problems, k+": listed in identicalInBothLanguages but the values differ; remove the entry")
		}
	}
	return problems
}

func quote(s string) string { return `"` + s + `"` }

func catalogMaps(t *testing.T) (en, pt map[string]string) {
	t.Helper()
	cat, err := New(LangPTBR)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return cat.messages[LangEN], cat.messages[LangPTBR]
}

func TestPTBRCatalogHasNoEnglishCopy(t *testing.T) {
	en, pt := catalogMaps(t)
	for _, p := range auditPTBR(en, pt) {
		t.Error(p)
	}
}

// The audit has to fail on the mistakes it exists for. These cases run the
// same function over a catalog with the mistake added.
func TestPTBRAuditRejectsEnglish(t *testing.T) {
	en, pt := catalogMaps(t)
	if base := auditPTBR(en, pt); len(base) != 0 {
		t.Fatalf("real catalog must be clean before mutating it: %v", base)
	}

	cases := []struct {
		name string
		key  string
		en   string
		pt   string
	}{
		{"new key with the English text in pt-BR", "player.volume", "Volume: %d", "Volume: %d"},
		{"English sentence in pt-BR", "player.fail.new", "Channel not found", "Channel not found: %s"},
		{"English word inside Portuguese", "status.new", "Loading", "Carregando error %s"},
		{"layout token left in English", "help.layout_new", "Layout: split", "Layout: Ctrl+W cicla split/chat/lista/foco"},
		{"empty pt-BR value", "status.empty", "Ready", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enM, ptM := clone(en), clone(pt)
			enM[tc.key], ptM[tc.key] = tc.en, tc.pt
			problems := auditPTBR(enM, ptM)
			var hit bool
			for _, p := range problems {
				if strings.HasPrefix(p, tc.key+":") {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("audit accepted %q = %q; problems: %v", tc.key, tc.pt, problems)
			}
		})
	}

	t.Run("stale allowlist entry", func(t *testing.T) {
		enM, ptM := clone(en), clone(pt)
		ptM["player.vol"] = "volume %d"
		var hit bool
		for _, p := range auditPTBR(enM, ptM) {
			hit = hit || strings.HasPrefix(p, "player.vol:")
		}
		if !hit {
			t.Fatal("a translated key must leave identicalInBothLanguages")
		}
	})
}

// Portuguese and English quoted commands, key chords and placeholders are not
// copy: they must not trip the word check.
func TestPTBRWordsIgnoresCommandsKeysAndPlaceholders(t *testing.T) {
	got := ptBRWords("Rode 'ttv-tui login --from-browser' ou Ctrl+W, veja %s e chat:read em id.twitch.tv (500)")
	want := []string{"rode", "ou", "veja", "e", "em"}
	if slash := ptBRWords("cicla split/chat/lista"); strings.Join(slash, " ") != "cicla split chat lista" {
		t.Fatalf("slash-separated words = %v", slash)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("ptBRWords = %v, want %v", got, want)
	}
}

// The reverse mistake: Portuguese left in the English catalog.
func TestENCatalogHasNoPortugueseCopy(t *testing.T) {
	en, _ := catalogMaps(t)
	// help.colors keeps the pt-BR color names only in the pt-BR value.
	for k, v := range en {
		if strings.ContainsAny(v, "ãõçÃÕÇ") {
			t.Errorf("%s: English value %q has Portuguese characters", k, v)
		}
	}
}

func clone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}
