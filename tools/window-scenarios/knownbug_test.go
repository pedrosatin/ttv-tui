package windowscenarios

import (
	"errors"
	"testing"
	"time"
)

// O mecanismo de bug conhecido, sem janela: erro reprova, defeito presente pula,
// defeito ausente no prazo inteiro pede a remoção da marca.
func TestBugConhecidoSeparaErroDefeitoECorrecao(t *testing.T) {
	const prazo, passo = 50 * time.Millisecond, time.Millisecond
	infra := errors.New("socket de teste sem resposta")

	v, err := judgeKnownBug(prazo, passo, func() (bool, error) { return false, infra })
	if v != bugBroken || !errors.Is(err, infra) {
		t.Errorf("verificação com erro: %v, %v; quer bugBroken com o erro", v, err)
	}
	v, err = judgeKnownBug(prazo, passo, func() (bool, error) { return true, nil })
	if v != bugStill || err != nil {
		t.Errorf("defeito presente: %v, %v; quer bugStill", v, err)
	}
	calls := 0
	v, err = judgeKnownBug(prazo, passo, func() (bool, error) { calls++; return false, nil })
	if v != bugGone || err != nil || calls < 2 {
		t.Errorf("defeito ausente: %v, %v depois de %d tentativas; quer bugGone no fim do prazo", v, err, calls)
	}
	// Um estado de antes do defeito não basta para dar a correção como feita.
	calls = 0
	v, _ = judgeKnownBug(prazo, passo, func() (bool, error) { calls++; return calls >= 3, nil })
	if v != bugStill {
		t.Errorf("defeito que aparece na terceira leitura: %v, quer bugStill", v)
	}
	// Erro depois de leituras sem defeito continua sendo erro.
	calls = 0
	v, _ = judgeKnownBug(prazo, passo, func() (bool, error) {
		calls++
		if calls == 3 {
			return false, infra
		}
		return false, nil
	})
	if v != bugBroken {
		t.Errorf("erro na terceira leitura: %v, quer bugBroken", v)
	}
}
