// Package fixture serve dados de fixture para os cenários da janela real: um
// GQL com o formato da Twitch, um servidor IRC local e um clipe de vídeo em
// YUV4MPEG2. Nada aqui vai à rede de fora; os servidores só escutam em
// 127.0.0.1.
//
// O produto só fala com estes servidores quando o processo recebe as variáveis
// de teste (TTV_TUI_TEST_GQL, TTV_TUI_TEST_IRC e TTV_TUI_TEST_KICK, lidas por
// internal/app).
package fixture
