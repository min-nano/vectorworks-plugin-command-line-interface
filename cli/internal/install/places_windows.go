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
	return filepath.Join(append([]string{syscall.UTF16ToString(unsafe.Slice(path, 32768))}, sub...)...), nil
}

// userTempDir は %LOCALAPPDATA%\Temp（%TEMP% の既定の場所）。環境変数 TMP / TEMP は読まない。
func userTempDir() (string, error) {
	return knownFolder(&folderLocalAppData, "Temp")
}

func defaultPluginsDir() (string, error) {
	return knownFolder(&folderRoamingAppData, "Nemetschek", "Vectorworks", "2026", "Plug-Ins")
}

func machineIsArm64() bool {
	return false
}

// target は windows-x64 だけ（Windows on ARM でも Vectorworks は x64 のエミュレーションで動く）。
func target(func() bool) (string, error) {
	return "windows-x64", nil
}
