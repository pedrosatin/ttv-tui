//go:build !windowtest

package main

func testSocketPath() string        { return "" }
func (a *app) startTest(string)     {}
func (a *app) resizeForTest(ctlMsg) {}
