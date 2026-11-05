# Homebrew tap

A fórmula é um ponto de partida fixado no commit 37b50d0 (versão provisória
0.0.0), compilado a partir do código-fonte. Ela ainda não foi publicada nem
validada no macOS. A release 0.1.9 não contém arquivos Darwin.

Após publicar uma release com suporte a macOS, atualize `url`, `version` e
`sha256` para o arquivo de código-fonte dessa tag e confira o hash baixado.
Teste `brew install --build-from-source ./packaging/homebrew/ttv-tui.rb` e
`brew test ttv-tui` em macOS. Crie o repositório `pedrosatin/homebrew-tap`,
copie a fórmula para `Formula/ttv-tui.rb` e publique-o. Então usuários poderão
instalar com `brew install pedrosatin/tap/ttv-tui`. Repita a atualização da
fórmula a cada release; o GoReleaser atual não publica no tap automaticamente.
