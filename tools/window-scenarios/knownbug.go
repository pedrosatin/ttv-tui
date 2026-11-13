package windowscenarios

import "time"

// bugVerdict é o resultado da verificação de um bug conhecido.
type bugVerdict int

const (
	// bugBroken: a verificação devolveu erro; o cenário não chegou ao ponto do bug.
	bugBroken bugVerdict = iota
	// bugStill: a janela mostrou o defeito.
	bugStill
	// bugGone: o defeito não apareceu em todo o prazo.
	bugGone
)

// judgeKnownBug repete check até o prazo. Um erro encerra na hora como
// bugBroken; o defeito presente encerra na hora como bugStill; o defeito ausente
// em todas as tentativas, até o fim do prazo, dá bugGone. Ausência só conta no
// fim porque um estado lido cedo demais ainda pode ser o de antes do defeito.
func judgeKnownBug(timeout, step time.Duration, check func() (bugPresent bool, err error)) (bugVerdict, error) {
	end := time.Now().Add(timeout)
	for {
		present, err := check()
		switch {
		case err != nil:
			return bugBroken, err
		case present:
			return bugStill, nil
		case time.Now().After(end):
			return bugGone, nil
		}
		time.Sleep(step)
	}
}
