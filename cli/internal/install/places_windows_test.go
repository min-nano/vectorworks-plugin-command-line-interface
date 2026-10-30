package install

import (
	"strings"
	"syscall"
	"testing"
)

// 知らない場所の ID なら、SHGetKnownFolderPath が断る。
func TestKnownFolderUnknownID(t *testing.T) {
	if _, err := knownFolder(&syscall.GUID{Data1: 1}); err == nil || !strings.Contains(err.Error(), "HRESULT") {
		t.Fatal(err)
	}
}

// powershell は %SystemRoot% の下のものを走らせる。SystemRoot が無ければ C:\Windows の下。
func TestPowerShellWithoutSystemRoot(t *testing.T) {
	t.Setenv("SystemRoot", "")
	o := Options{}
	argv, err := o.interpreter("powershell")
	if err != nil || !strings.HasPrefix(argv[0], `C:\Windows\System32\WindowsPowerShell`) {
		t.Fatalf("%v %v", argv, err)
	}
}
