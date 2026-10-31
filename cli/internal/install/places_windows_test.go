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

// powershell は FOLDERID_System（System32）の下のものを走らせ、環境変数 SystemRoot は読まない。
func TestPowerShellIgnoresSystemRoot(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("SystemRoot", elsewhere)
	o := Options{}
	argv, err := o.interpreter("powershell")
	if err != nil || strings.HasPrefix(argv[0], elsewhere) ||
		!strings.HasSuffix(strings.ToLower(argv[0]), `\system32\windowspowershell\v1.0\powershell.exe`) {
		t.Fatalf("%v %v", argv, err)
	}
}
