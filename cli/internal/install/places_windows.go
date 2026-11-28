package install

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

var (
	shell32                  = syscall.NewLazyDLL("shell32.dll")
	ole32                    = syscall.NewLazyDLL("ole32.dll")
	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")

	// FOLDERID_LocalAppData
	folderLocalAppData = syscall.GUID{Data1: 0xF1B32785, Data2: 0x6FBA, Data3: 0x4FCF, Data4: [8]byte{0x9D, 0x55, 0x7B, 0x8E, 0x7F, 0x15, 0x70, 0x91}}
	// FOLDERID_RoamingAppData
	folderRoamingAppData = syscall.GUID{Data1: 0x3EB685DB, Data2: 0x65F9, Data3: 0x4CF6, Data4: [8]byte{0xA0, 0x3A, 0xE3, 0xEF, 0x65, 0x72, 0x9F, 0x3D}}
)

// knownFolder は SHGetKnownFolderPath の場所の下の sub。環境変数（LOCALAPPDATA・APPDATA）は
// 読み取らない。
func knownFolder(id *syscall.GUID, sub ...string) (string, error) {
	var path *uint16
	hr, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(id)), 0, 0, uintptr(unsafe.Pointer(&path)))
	if path != nil {
		defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(path)))
	}
	if hr != 0 {
		return "", fmt.Errorf("SHGetKnownFolderPath: HRESULT 0x%08x", uint32(hr))
	}
	return filepath.Join(append([]string{utf16PtrToString(path)}, sub...)...), nil
}

// utf16PtrToString は、NUL で終わる UTF-16 の文字列を読み取る。SHGetKnownFolderPath が返す領域は
// 文字列の長さしか無いため、先に NUL までの長さを計数し、その長さのみのスライスを生成する（領域の
// 外を覆わない）。
func utf16PtrToString(p *uint16) string {
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; ptr = unsafe.Add(ptr, 2) {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// userTempDir は %LOCALAPPDATA%\Temp（%TEMP% の既定の場所）。環境変数 TMP / TEMP は読み取らない。
func userTempDir() (string, error) {
	return knownFolder(&folderLocalAppData, "Temp")
}

func defaultPluginsDir() (string, error) {
	return knownFolder(&folderRoamingAppData, "Nemetschek", "Vectorworks", edition.Year, "Plug-Ins")
}

// target は windows-x64 のみ（Windows on ARM でも Vectorworks は x64 のエミュレーションで
// 動作する）。
func target() string {
	return "windows-x64"
}

// FOLDERID_System
var folderSystem = syscall.GUID{Data1: 0x1AC14E77, Data2: 0x02E7, Data3: 0x4E5D, Data4: [8]byte{0xB7, 0x44, 0x2E, 0xB1, 0xAE, 0x51, 0x98, 0xB7}}

// defaultInterpreter は、フックの 1 つ目を実行する argv の先頭。Windows は Windows PowerShell
// 5.1 のみで、場所は FOLDERID_System（System32）から導出する（環境変数 SystemRoot は
// 読み取らない）。
func defaultInterpreter(name string) ([]string, error) {
	if name != "powershell" {
		return nil, fmt.Errorf("%s hooks do not run on Windows", name)
	}
	path, err := knownFolder(&folderSystem, "WindowsPowerShell", "v1.0", "powershell.exe")
	return []string{path, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File"}, err
}
