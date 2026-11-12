//go:build windows

package main

import (
	"io"
	"net"
	"testing"
)

type udpWriter struct {
	conn net.Conn
}

func (u udpWriter) Write(p []byte) (int, error) {
	return u.conn.Write(p)
}

func (u udpWriter) Close() error {
	return u.conn.Close()
}

type packetReader struct {
	net.PacketConn
}

func (p *packetReader) Read(b []byte) (int, error) {
	n, _, err := p.PacketConn.ReadFrom(b)
	return n, err
}

func (p *packetReader) Write(b []byte) (int, error) {
	return len(b), nil
}

func (p *packetReader) Close() error {
	return p.PacketConn.Close()
}

func seqpacketPair(t *testing.T) (ptyDevice, io.WriteCloser) {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.Dial("udp", server.LocalAddr().String())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	r := &packetReader{PacketConn: server}
	w := udpWriter{conn: client}
	t.Cleanup(func() { _, _ = r.Close(), w.Close() })
	return mockPTYDevice{r}, w
}
