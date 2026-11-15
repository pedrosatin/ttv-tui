package ui

import (
	"strings"
	"testing"
)

func TestHostedHeightLimitsAndMigration(t *testing.T) {
	m := hostedModel(t, 120, 41, true)
	m.hosted.chatOverlay = true
	m.resizeHostedHeight(-2)
	if got := m.overlayHeight(40); got != 38 {
		t.Fatalf("altura %d", got)
	}
	if !strings.Contains(m.notices.status, m.t("layout.opacity_moved")) {
		t.Fatal(m.notices.status)
	}
	m.resizeHostedHeight(-2)
	if strings.Contains(m.notices.status, m.t("layout.opacity_moved")) {
		t.Fatal("aviso repetido")
	}
	for range 100 {
		m.resizeHostedHeight(-2)
	}
	if m.overlayHeight(40) != 6 {
		t.Fatal("mínimo")
	}
	for range 100 {
		m.resizeHostedHeight(2)
	}
	if m.overlayHeight(40) != 40 {
		t.Fatal("máximo")
	}
}

func TestCenarioChatSobrepostoAltura(t *testing.T) {
	eachSize(t, "chat-sobreposto-altura", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenarioClassic(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
		m = chatLines(t, m, [2]string{"Fulano", "salve, chat"}, [2]string{"Ciclana", "que jogada"})
		m = play(t, m, "alt+down alt+down alt+pgup")
		return m
	})
}

func TestCenarioChatSobrepostoMigracao(t *testing.T) {
	eachSize(t, "chat-sobreposto-migracao", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenarioClassic(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter ctrl+o alt+down")
		return m
	})
}

func TestCenarioEscritaNoChatSobreposto(t *testing.T) {
	eachSize(t, "chat-sobreposto-escrita", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenarioClassic(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
		m = chatLines(t, m, [2]string{"Fulano", "salve, chat"})
		m = play(t, m, "i mensagem")
		if !m.chat.composing || m.chat.composeDraft != "mensagem" {
			t.Fatal("a escrita não abriu no overlay")
		}
		return m
	})
}

func TestHostedHeightSaveRestoresNoticeAndBackdrop(t *testing.T) {
	m := hostedModel(t, 120, 41, true)
	m.hosted.chatOverlay = true
	m.hosted.overlayPreferences.TextBackdrop = true
	var saved OverlayPreferences
	m.hosted.persist = func(_, _ int, _, _ bool, _ int, p OverlayPreferences) error { saved = p; return nil }
	m.resizeHostedHeight(-2)
	m.hosted.saveColumns(m.hosted.saveGen)()
	restored := hostedModel(t, 120, 41, true)
	restored.hosted.chatOverlay = true
	WithHostedOverlay(saved)(&restored)
	if restored.overlayHeight(40) != 38 || !restored.hostedPanel().OverlayTextBackdrop {
		t.Fatal("preferências não restauradas")
	}
	restored.resizeHostedHeight(-2)
	if strings.Contains(restored.notices.status, restored.t("layout.opacity_moved")) {
		t.Fatal("migração repetida depois de restaurar")
	}
}
