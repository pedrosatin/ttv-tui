package store

import (
	"fmt"
	"slices"
	"testing"
)

// A lista gravada põe na frente os abertos, do mais novo ao mais antigo; num
// estado que ainda não confirmou recentes, a lista local vem em seguida; o
// disco completa, sem os removidos; e o limite vale só no fim.
func TestMergeRecents(t *testing.T) {
	ch := func(logins ...string) []Channel {
		out := make([]Channel, 0, len(logins))
		for _, l := range logins {
			out = append(out, Channel{Login: l})
		}
		return out
	}
	for _, tc := range []struct {
		name                           string
		opened, forgotten, local, disk []Channel
		max                            int
		want                           []string
	}{
		{"nada mudou", nil, nil, nil, ch("d", "a", "b"), 50, []string{"d", "a", "b"}},
		{"abriu um do meio", ch("b"), nil, nil, ch("d", "a", "b", "c"), 50, []string{"b", "d", "a", "c"}},
		{"abriu o que já era o primeiro", ch("a"), nil, nil, ch("b"), 50, []string{"a", "b"}},
		{"tirou um", nil, ch("b"), nil, ch("a", "b", "c"), 50, []string{"a", "c"}},
		{"sem confirmação, a lista local entra", ch("new"), nil, ch("new", "b"), ch("a", "b", "c"), 50, []string{"new", "b", "a", "c"}},
		{"sem confirmação, o removido não volta pela lista local", nil, ch("b"), ch("a", "b"), ch("b", "c"), 50, []string{"a", "c"}},
		{"o limite vem depois da mescla", ch("n2", "n1"), ch("a"), nil, ch("a", "b", "c"), 3, []string{"n2", "n1", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := recentLogins(State{Recents: mergeRecents(tc.opened, tc.forgotten, tc.local, tc.disk, tc.max)})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("mergeRecents = %q, quer %q", got, tc.want)
			}
		})
	}
}

// O RecentLog guarda só o último evento de cada canal, e pending devolve os
// posteriores à confirmação, abertos do mais novo ao mais antigo.
func TestRecentLog(t *testing.T) {
	var log RecentLog
	log.Opened(Channel{Login: "a"})
	acked := log.lastSeq()
	log.Opened(Channel{Login: "b"})
	log.Forgot(Channel{Login: "A"})
	log.Opened(Channel{Platform: "kick", Login: "b"})
	log.Forgot(Channel{Login: "c"})
	log.Opened(Channel{Login: "c"})

	copied := log
	copied.Forgot(Channel{Login: "b"})

	opened, forgotten := log.pending(acked)
	if got := recentSpecs(State{Recents: opened}); !slices.Equal(got, []string{"c", "kick/b", "b"}) {
		t.Fatalf("abertos = %q", got)
	}
	if got := recentSpecs(State{Recents: forgotten}); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("removidos = %q", got)
	}
	if opened, forgotten := log.pending(log.lastSeq()); len(opened)+len(forgotten) != 0 {
		t.Fatalf("pendentes depois da última confirmação: %q %q", recentSpecs(State{Recents: opened}), recentSpecs(State{Recents: forgotten}))
	}
	// A cópia registra o próprio evento sem mudar o original.
	if _, forgotten := copied.pending(acked); !slices.Equal(recentSpecs(State{Recents: forgotten}), []string{"b", "A"}) {
		t.Fatalf("removidos na cópia = %q", recentSpecs(State{Recents: forgotten}))
	}
}

// Prune tira os eventos já confirmados e deixa os pendentes iguais; uma cópia
// tirada antes continua com os dela. Numa sessão longa, com cada abertura
// gravada e confirmada, o diário não cresce.
func TestRecentLogPrune(t *testing.T) {
	var log RecentLog
	log.Opened(Channel{Login: "a"})
	log.Forgot(Channel{Login: "b"})
	acked := log.LastSeq()
	log.Opened(Channel{Login: "c"})
	before := log

	log.Prune(acked)
	if log.Len() != 1 {
		t.Fatalf("diário com %d canais depois do Prune, quero só o pendente", log.Len())
	}
	opened, forgotten := log.pending(acked)
	if got := recentSpecs(State{Recents: opened}); !slices.Equal(got, []string{"c"}) || len(forgotten) != 0 {
		t.Fatalf("pendentes depois do Prune: abertos %q, removidos %d", got, len(forgotten))
	}
	if before.Len() != 3 {
		t.Fatalf("a cópia anterior ficou com %d canais, quero 3", before.Len())
	}

	var long RecentLog
	for i := range 1000 {
		long.Opened(Channel{Login: fmt.Sprintf("canal%04d", i)})
		long.Prune(long.LastSeq())
	}
	if long.Len() != 0 {
		t.Fatalf("diário com %d canais depois de 1000 aberturas confirmadas", long.Len())
	}
}
