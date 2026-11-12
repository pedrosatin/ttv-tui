package main

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

type mockPTYDevice struct {
	io.ReadWriteCloser
}

func (m mockPTYDevice) Resize(cols, rows, pw, ph int) error {
	return nil
}

// chunkPipe imita o PTY entregando cada escrita numa leitura própria, como um
// socket SOCK_SEQPACKET, em qualquer sistema. Fechar o escritor vira EOF.
type chunkPipe struct {
	chunks chan []byte
	once   sync.Once
	rest   []byte
}

func (p *chunkPipe) Read(b []byte) (int, error) {
	if len(p.rest) == 0 {
		c, ok := <-p.chunks
		if !ok {
			return 0, io.EOF
		}
		p.rest = c
	}
	n := copy(b, p.rest)
	p.rest = p.rest[n:]
	return n, nil
}

func (p *chunkPipe) Write(b []byte) (int, error) {
	p.chunks <- bytes.Clone(b)
	return len(b), nil
}

func (p *chunkPipe) Close() error {
	p.once.Do(func() { close(p.chunks) })
	return nil
}

func seqpacketPair(t *testing.T) (ptyDevice, io.WriteCloser) {
	t.Helper()
	p := &chunkPipe{chunks: make(chan []byte, 64)}
	t.Cleanup(func() { _ = p.Close() })
	return mockPTYDevice{p}, p
}
