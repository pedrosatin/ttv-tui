package i18n

import "testing"

func TestResolvePrecedenceAndNormalize(t *testing.T) {
	cases := []struct {
		name       string
		flagLang   string
		configLang string
		envLocale  string
		want       string
	}{
		{
			name:       "flag overrides config and env",
			flagLang:   "pt-BR",
			configLang: "en",
			envLocale:  "en_US.UTF-8",
			want:       "pt-BR",
		},
		{
			name:       "config overrides env",
			flagLang:   "",
			configLang: "pt-BR",
			envLocale:  "en_US.UTF-8",
			want:       "pt-BR",
		},
		{
			name:       "env pt_BR.UTF-8 normalizes to pt-BR",
			flagLang:   "",
			configLang: "",
			envLocale:  "pt_BR.UTF-8",
			want:       "pt-BR",
		},
		{
			name:       "env pt normalizes to pt-BR",
			flagLang:   "",
			configLang: "",
			envLocale:  "pt",
			want:       "pt-BR",
		},
		{
			name:       "unknown locale falls back to en",
			flagLang:   "",
			configLang: "",
			envLocale:  "fr_FR.UTF-8",
			want:       "en",
		},
		{
			name:       "all empty falls back to en",
			flagLang:   "",
			configLang: "",
			envLocale:  "",
			want:       "en",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.flagLang, tc.configLang, tc.envLocale)
			if got != tc.want {
				t.Fatalf("Resolve(%q, %q, %q) = %q, want %q", tc.flagLang, tc.configLang, tc.envLocale, got, tc.want)
			}
		})
	}
}

func TestCatalogTranslationAndFallbackToEN(t *testing.T) {
	catEN, err := New(LangEN)
	if err != nil {
		t.Fatalf("New(en): %v", err)
	}
	catPT, err := New(LangPTBR)
	if err != nil {
		t.Fatalf("New(pt-BR): %v", err)
	}

	if got := catEN.T("status.ready"); got != "Ready" {
		t.Fatalf("en status.ready = %q, want %q", got, "Ready")
	}
	if got := catPT.T("status.ready"); got != "Pronto" {
		t.Fatalf("pt-BR status.ready = %q, want %q", got, "Pronto")
	}

	// Verify fallback to English when a key is missing in pt-BR.
	custom := newFromMaps(LangPTBR, map[string]map[string]string{
		LangEN: {
			"only.in.en": "English Fallback Value",
		},
		LangPTBR: {},
	})
	if got := custom.T("only.in.en"); got != "English Fallback Value" {
		t.Fatalf("missing key in pt-BR returned %q, want %q", got, "English Fallback Value")
	}
}

func extractPlaceholders(s string) []string {
	var verbs []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++ // skip %%
			continue
		}
		// Scan format specifier
		j := i + 1
		for j < len(s) && (s[j] == '+' || s[j] == '-' || s[j] == '#' || s[j] == ' ' || s[j] == '0' || (s[j] >= '0' && s[j] <= '9') || s[j] == '.') {
			j++
		}
		if j < len(s) {
			verbs = append(verbs, s[i:j+1])
			i = j
		}
	}
	return verbs
}

func TestCatalogKeyAndPlaceholderParity(t *testing.T) {
	cat, err := New(LangEN)
	if err != nil {
		t.Fatalf("New(en): %v", err)
	}
	enKeys := cat.messages[LangEN]
	ptKeys := cat.messages[LangPTBR]
	if len(enKeys) == 0 {
		t.Fatal("en catalog is empty")
	}

	for k, enVal := range enKeys {
		if enVal == "" {
			t.Errorf("en key %q has empty value", k)
		}
		ptVal, ok := ptKeys[k]
		if !ok || ptVal == "" {
			t.Errorf("pt-BR catalog missing or empty key %q", k)
			continue
		}
		enPlaceholders := extractPlaceholders(enVal)
		ptPlaceholders := extractPlaceholders(ptVal)
		if len(enPlaceholders) != len(ptPlaceholders) {
			t.Errorf("key %q placeholder count mismatch: en=%v pt-BR=%v", k, enPlaceholders, ptPlaceholders)
			continue
		}
		for idx := range enPlaceholders {
			if enPlaceholders[idx] != ptPlaceholders[idx] {
				t.Errorf("key %q placeholder [%d] mismatch: en=%q pt-BR=%q", k, idx, enPlaceholders[idx], ptPlaceholders[idx])
			}
		}
	}

	for k := range ptKeys {
		if _, ok := enKeys[k]; !ok {
			t.Errorf("pt-BR key %q orphan (missing from mandatory en catalog)", k)
		}
	}
}
