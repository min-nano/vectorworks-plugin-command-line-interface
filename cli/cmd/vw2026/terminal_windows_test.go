package main

import (
	"os"
	"testing"
)

// openTerminal は、Windows のテストにはコンソールが無いので見ない。
func openTerminal(t *testing.T) *os.File {
	t.Helper()
	t.Skip("the tests on Windows have no console")
	return nil
}
