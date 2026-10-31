package install

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
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

// knownFolder は SHGetKnownFolderPath の場所の下の sub。環境変数（LOCALAPPDATA・APPDATA）は読まない。
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

// utf16PtrToString は、NUL で終わる UTF-16 の文字列を読む。SHGetKnownFolderPath が返す領域は文字列の
// 長さしかないので、先に NUL までの長さを数え、その長さだけのスライスを作る（領域の外を覆わない）。
func utf16PtrToString(p *uint16) string {
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; ptr = unsafe.Add(ptr, 2) {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// userTempDir は %LOCALAPPDATA%\Temp（%TEMP% の既定の場所）。環境変数 TMP / TEMP は読まない。
func userTempDir() (string, error) {
	return knownFolder(&folderLocalAppData, "Temp")
}

func defaultPluginsDir() (string, error) {
	return knownFolder(&folderRoamingAppData, "Nemetschek", "Vectorworks", "2026", "Plug-Ins")
}

// target は windows-x64 だけ（Windows on ARM でも Vectorworks は x64 のエミュレーションで動く）。
func target() string {
	return "windows-x64"
}

// oldCLIName は、よけた CLI の名前の頭（oldCLIPrefixes の 1 つ）。
const oldCLIName = "vw2026.exe.old-"

// FOLDERID_System
var folderSystem = syscall.GUID{Data1: 0x1AC14E77, Data2: 0x02E7, Data3: 0x4E5D, Data4: [8]byte{0xB7, 0x44, 0x2E, 0xB1, 0xAE, 0x51, 0x98, 0xB7}}

// defaultInterpreter は、フックの 1 つ目を走らせる argv の頭。Windows は Windows PowerShell 5.1 だけで、
// 場所は FOLDERID_System（System32）から求める（環境変数 SystemRoot は読まない）。
func defaultInterpreter(name string) ([]string, error) {
	if name != "powershell" {
		return nil, fmt.Errorf("%s hooks do not run on Windows", name)
	}
	path, err := knownFolder(&folderSystem, "WindowsPowerShell", "v1.0", "powershell.exe")
	return []string{path, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File"}, err
}
