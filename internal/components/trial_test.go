package components

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	versaoAnterior = "8.6.3-1"
	versaoNova     = "8.7.0"
)

var errInterrompido = errors.New("processo interrompido neste passo")

// copiaComoProcessoMorto copia o diretório de dados como um SIGKILL o deixaria:
// sem rodar nenhum defer, com os locks e marcadores do processo morto envelhecidos
// para que um processo novo os trate como abandonados.
func copiaComoProcessoMorto(t *testing.T, origem string) string {
	t.Helper()
	destino := t.TempDir()
	velho := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(origem, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(origem, p)
		if err != nil {
			return err
		}
		alvo := filepath.Join(destino, rel)
		info, err := e.Info()
		if err != nil {
			return err
		}
		if e.IsDir() {
			return os.MkdirAll(alvo, info.Mode().Perm())
		}
		if err := copiaArquivo(p, alvo, info.Mode().Perm()); err != nil {
			return err
		}
		if strings.HasPrefix(e.Name(), ".") {
			return os.Chtimes(alvo, velho, velho)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("copiar o estado interrompido: %v", err)
	}
	return destino
}

func copiaArquivo(origem, destino string, modo os.FileMode) error {
	in, err := os.Open(origem)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, modo)
	if err != nil {
		return err
	}
	_, cerr := io.Copy(out, in)
	return errors.Join(cerr, out.Close())
}

// confereEstadoConsistente verifica o contrato da atualização num diretório de
// dados: a versão atual é a anterior, ou é a nova em teste, e nesse caso a volta
// para a anterior funciona.
func confereEstadoConsistente(t *testing.T, dataDir string, atualEsperada string) {
	t.Helper()
	atual, _ := linuxResolver(dataDir).ManagedVersions()
	st := LoadUpdateState(dataDir)
	if atual != atualEsperada {
		t.Fatalf("versão atual = %q, quer %q (estado %+v)", atual, atualEsperada, st)
	}
	switch atual {
	case versaoAnterior:
		// Nada a desfazer: a anterior continua em uso.
	case versaoNova:
		if st.Trial != versaoNova {
			t.Fatalf("a versão nova é a atual sem estar em teste (estado %+v)", st)
		}
		d := newUpdateDownloader(t, newReleaseServer(t, versaoNova), dataDir, newClock())
		volta, err := d.MarkStreamlinkBad(context.Background(), versaoNova)
		if err != nil || volta.Version != versaoAnterior {
			t.Fatalf("volta para a anterior = %+v, %v; quer %s", volta, err, versaoAnterior)
		}
	default:
		t.Fatalf("versão atual inesperada %q", atual)
	}
}

// TestAtualizacaoInterrompidaEmCadaPassoFicaReversivel interrompe a atualização
// em cada ponto da sequência de gravação, como processo morto (cópia do disco
// sem defers) e como erro injetado (defers rodam), e confere que o estado em
// disco nunca deixa a versão nova atual sem teste. Depois, uma checagem nova
// completa a atualização a partir do estado interrompido.
func TestAtualizacaoInterrompidaEmCadaPassoFicaReversivel(t *testing.T) {
	casos := []struct {
		passo installStep
		// atualMorto e trialMorto: o disco depois de um SIGKILL no passo.
		atualMorto, trialMorto string
		// atualErro e trialErro: o disco depois de um erro devolvido no passo.
		atualErro, trialErro string
	}{
		{stepChecked, versaoAnterior, "", versaoAnterior, ""},
		{stepStaged, versaoAnterior, "", versaoAnterior, ""},
		// Morto depois de gravar o teste e antes do rename: o teste aponta para
		// uma versão ausente, que nenhuma reprodução confirma nem desfaz.
		{stepTrial, versaoAnterior, versaoNova, versaoAnterior, ""},
		{stepPromoted, versaoNova, versaoNova, versaoNova, versaoNova},
		{stepPruned, versaoNova, versaoNova, versaoNova, versaoNova},
	}
	for _, c := range casos {
		t.Run(string(c.passo)+"/processo morto", func(t *testing.T) {
			dataDir := t.TempDir()
			installFakeVersion(t, dataDir, versaoAnterior)
			rs := newReleaseServer(t, versaoNova)
			clock := newClock()
			d := newUpdateDownloader(t, rs, dataDir, clock)
			// Duas cópias do mesmo instante: a conferência da volta altera uma.
			var copia, retomar string
			d.afterStep = func(s installStep) error {
				if s != c.passo {
					return nil
				}
				copia = copiaComoProcessoMorto(t, dataDir)
				retomar = copiaComoProcessoMorto(t, dataDir)
				return errInterrompido
			}
			if _, err := d.UpdateStreamlink(context.Background()); !errors.Is(err, errInterrompido) {
				t.Fatalf("UpdateStreamlink = %v, quer a interrupção", err)
			}
			if got := LoadUpdateState(copia).Trial; got != c.trialMorto {
				t.Fatalf("trial no disco = %q, quer %q", got, c.trialMorto)
			}
			confereEstadoConsistente(t, copia, c.atualMorto)

			// A próxima checagem, num processo novo, completa a atualização.
			clock.advance(UpdateInterval + time.Hour)
			retomada := newUpdateDownloader(t, rs, retomar, clock)
			// O lock do processo morto faz a primeira tentativa ceder; ela o
			// remove por velho e a seguinte instala.
			res, err := retomada.UpdateStreamlink(context.Background())
			if err == nil && res.Status == UpdateBusy {
				res, err = retomada.UpdateStreamlink(context.Background())
			}
			if err != nil || res.Status == UpdateBusy {
				t.Fatalf("checagem seguinte = %+v, %v", res, err)
			}
			atual, anterior := linuxResolver(retomar).ManagedVersions()
			if st := LoadUpdateState(retomar); atual != versaoNova || anterior != versaoAnterior || st.Trial != versaoNova {
				t.Fatalf("depois da retomada: atual %q, anterior %q, estado %+v", atual, anterior, st)
			}
		})
		t.Run(string(c.passo)+"/erro injetado", func(t *testing.T) {
			dataDir := t.TempDir()
			installFakeVersion(t, dataDir, versaoAnterior)
			d := newUpdateDownloader(t, newReleaseServer(t, versaoNova), dataDir, newClock())
			d.afterStep = func(s installStep) error {
				if s == c.passo {
					return errInterrompido
				}
				return nil
			}
			if _, err := d.UpdateStreamlink(context.Background()); !errors.Is(err, errInterrompido) {
				t.Fatalf("UpdateStreamlink = %v, quer a interrupção", err)
			}
			if got := LoadUpdateState(dataDir).Trial; got != c.trialErro {
				t.Fatalf("trial = %q, quer %q", got, c.trialErro)
			}
			confereEstadoConsistente(t, dataDir, c.atualErro)
		})
	}
}

// TestAtualizacaoCanceladaAntesDoRenameNaoPromove cancela o contexto do app
// entre a gravação do teste e o rename: nada é promovido e o teste anterior,
// de uma versão ainda não confirmada, volta.
func TestAtualizacaoCanceladaAntesDoRenameNaoPromove(t *testing.T) {
	casos := []struct {
		nome          string
		trialAnterior string
	}{
		{"sem teste anterior", ""},
		{"anterior ainda em teste", versaoAnterior},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			dataDir := t.TempDir()
			installFakeVersion(t, dataDir, versaoAnterior)
			if err := saveUpdateState(dataDir, UpdateState{Trial: c.trialAnterior}); err != nil {
				t.Fatal(err)
			}
			d := newUpdateDownloader(t, newReleaseServer(t, versaoNova), dataDir, newClock())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d.afterStep = func(s installStep) error {
				if s == stepTrial {
					cancel()
				}
				return nil
			}
			if _, err := d.UpdateStreamlink(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("UpdateStreamlink = %v, quer context.Canceled", err)
			}
			if versionDirExists(dataDir, versaoNova) {
				t.Fatal("a versão nova foi promovida depois do cancelamento")
			}
			if got := LoadUpdateState(dataDir).Trial; got != c.trialAnterior {
				t.Fatalf("trial = %q, quer o anterior %q", got, c.trialAnterior)
			}
			confereEstadoConsistente(t, dataDir, versaoAnterior)
		})
	}
}

// TestDownloadSemTamanhoPositivoRecusado serve uma release cujo asset tem
// tamanho zero, negativo ou ausente: o download é recusado antes de baixar
// qualquer byte, e nada é instalado.
func TestDownloadSemTamanhoPositivoRecusado(t *testing.T) {
	casos := []struct {
		nome    string
		tamanho string // trecho JSON do campo size, vazio para omitir
	}{
		{"size zero", `"size": 0,`},
		{"size ausente", ""},
		{"size negativo", `"size": -1,`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			assetHits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/releases/latest":
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"tag_name": "8.7.0", "assets": [{
						"name": "streamlink-8.7.0-cp315-cp315-manylinux_2_28_x86_64.AppImage",
						"browser_download_url": "http://`+r.Host+`/assets/streamlink.AppImage",
						`+c.tamanho+`
						"digest": "sha256:`+strings.Repeat("0", 64)+`"}]}`)
				default:
					assetHits++
					_, _ = io.WriteString(w, "conteúdo sem limite")
				}
			}))
			defer srv.Close()
			u, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			dataDir := t.TempDir()
			d := &Downloader{
				Transport:    srv.Client().Transport,
				AllowedHosts: []string{u.Host},
				APIBaseURL:   srv.URL + "/api/releases/latest",
				DataDir:      dataDir,
				GOOS:         "linux",
				GOARCH:       "amd64",
				InsecureHTTP: true,
				LookPath:     func(string) (string, error) { return "", os.ErrNotExist },
			}
			_, err = d.DownloadStreamlink(context.Background(), nil)
			if err == nil || !strings.Contains(err.Error(), "no positive size") {
				t.Fatalf("erro = %v, quer a recusa por tamanho", err)
			}
			if assetHits != 0 {
				t.Fatalf("o asset foi pedido %d vez(es) sem tamanho conhecido", assetHits)
			}
			if versionDirExists(dataDir, versaoNova) {
				t.Fatal("versão instalada sem tamanho conhecido")
			}
		})
	}
}

// TestDownloadAndVerifyExigeTamanhoPositivo cobre a defesa no próprio download,
// para quem chamar downloadAndVerify sem passar por fetchLatest.
func TestDownloadAndVerifyExigeTamanhoPositivo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("o download não devia ser pedido")
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	d := &Downloader{Transport: srv.Client().Transport, AllowedHosts: []string{u.Host}, InsecureHTTP: true}
	for _, tamanho := range []int64{0, -1} {
		destino := filepath.Join(t.TempDir(), "asset")
		err := d.downloadAndVerify(context.Background(), srv.URL+"/a", destino, tamanho, strings.Repeat("0", 64), nil)
		if err == nil || !strings.Contains(err.Error(), "positive expected size") {
			t.Fatalf("tamanho %d: erro = %v, quer a recusa", tamanho, err)
		}
		if _, err := os.Stat(destino); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("tamanho %d: arquivo criado sem tamanho conhecido", tamanho)
		}
	}
}

func TestValidateTagNameAceitaSoNomesSegurosEmQualquerSistema(t *testing.T) {
	casos := []struct {
		tag    string
		aceito bool
	}{
		{"8.6.3-1", true},
		{"v8.7.0+build", true},
		{"7.1.3_2", true},
		{"8", true},
		{"CONSOLE", true},
		{"COM10", true},
		{"nulo-1", true},
		{"", false},
		{".", false},
		{"..", false},
		{"8..7", false},
		{".8.7", false},
		{"8.7.", false},
		{"8.7/../x", false},
		{`8.7\x`, false},
		{"8.7:x", false},
		{"C:", false},
		{"8.7\x00", false},
		{"8.7\n", false},
		{"8.7\x1b", false},
		{"8.7 0", false},
		{"8.7é", false},
		{"8.7*", false},
		{"CON", false},
		{"con", false},
		{"nul.txt", false},
		{"NUL.8.7", false},
		{"Aux", false},
		{"PRN", false},
		{"COM1", false},
		{"lpt9", false},
		{"COM0", false},
		{"CONIN$", false},
		{strings.Repeat("1", maxTagNameLen), true},
		{strings.Repeat("1", maxTagNameLen+1), false},
	}
	for _, c := range casos {
		err := validateTagName(c.tag)
		if (err == nil) != c.aceito {
			t.Errorf("validateTagName(%q) = %v, aceito quer %v", c.tag, err, c.aceito)
		}
	}
}

// TestReleaseComTagInvalidaNaoInstala confere que o validador barra a tag no
// caminho real, antes de qualquer diretório de versão.
func TestReleaseComTagInvalidaNaoInstala(t *testing.T) {
	for _, tag := range []string{"NUL", "8.7:0", "8.7\x01"} {
		dataDir := t.TempDir()
		installFakeVersion(t, dataDir, versaoAnterior)
		rs := newReleaseServer(t, tag)
		d := newUpdateDownloader(t, rs, dataDir, newClock())
		_, err := d.UpdateStreamlink(context.Background())
		if err == nil || !strings.Contains(err.Error(), "invalid release tag") {
			t.Fatalf("tag %q: erro = %v, quer a recusa da tag", tag, err)
		}
		if _, assets := rs.hits(); assets != 0 {
			t.Fatalf("tag %q: o asset foi baixado", tag)
		}
		if atual, _ := linuxResolver(dataDir).ManagedVersions(); atual != versaoAnterior {
			t.Fatalf("tag %q: versão atual = %q", tag, atual)
		}
	}
}
