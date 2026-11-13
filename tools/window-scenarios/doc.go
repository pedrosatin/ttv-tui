// Package windowscenarios roda os cenários de UX na janela real: compila
// ttv-tui, ttv-tui-window e o streamlink falso, abre a janela com
// SDL_VIDEO_DRIVER=offscreen, sem compositor, com o GQL, o IRC e o clipe de
// internal/fixture, e conduz tudo pelo socket de teste da janela.
//
// Os cenários só compilam com a tag cenarios, no Linux:
//
//	go test -tags cenarios -count=1 ./tools/window-scenarios
//
// Sem a tag roda só o teste do mecanismo de bug conhecido (knownbug_test.go).
//
// O guia está em docs/cenarios-de-ux.md, na seção da janela.
package windowscenarios
