package main

import (
	"os"
	"testing"
)

// openTerminal は、Windows のテストにはコンソールが無いため検証しない。
func openTerminal(t *testing.T) *os.File {
	t.Helper()
	t.Skip("the tests on Windows have no console")
	return nil
}
