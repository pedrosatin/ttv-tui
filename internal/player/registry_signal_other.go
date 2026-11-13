//go:build !linux

package player

// osSignalPlayerGroup confere a identidade e sinaliza o grupo pelo PGID. Fora do
// Linux não há pidfd: no macOS a janela entre a conferência e o sinal fica
// aberta, e no Windows groupIsPlayer nunca confirma um player, então nada é
// sinalizado.
func osSignalPlayerGroup(pgid int, socket string, sig groupSignal) (bool, error) {
	return signalCheckedGroup(pgid, socket, sig)
}
