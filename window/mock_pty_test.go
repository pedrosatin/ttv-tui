package main

import "io"

type mockPTYDevice struct {
	io.ReadWriteCloser
}

func (m mockPTYDevice) Resize(cols, rows, pw, ph int) error {
	return nil
}
