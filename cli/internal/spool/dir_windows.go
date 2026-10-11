//go:build windows

package spool

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32                  = syscall.NewLazyDLL("shell32.dll")
	ole32                    = syscall.NewLazyDLL("ole32.dll")
	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")

	// folderIDLocalAppData は FOLDERID_LocalAppData（{F1B32785-6FBA-4FCF-9D55-7B8E7F157091}）。
	folderIDLocalAppData = syscall.GUID{
		Data1: 0xF1B32785, Data2: 0x6FBA, Data3: 0x4FCF,
		Data4: [8]byte{0x9D, 0x55, 0x7B, 0x8E, 0x7F, 0x15, 0x70, 0x91},
	}
)

// appRoot は <CLI> の親（%LOCALAPPDATA% の指す場所）を返す。
//
// プラグインと同じく SHGetKnownFolderPath(FOLDERID_LocalAppData) で求め、環境変数
// LOCALAPPDATA は読まない。環境変数は起動のしかたで変わりうるので、プラグインと食い違うと
// 要求が届かなくなるため（docs/protocol.md「スプールの場所」）。
func appRoot() (string, error) {
	var path *uint16
	hr, _, _ := procSHGetKnownFolderPath.Call(
		uintptr(unsafe.Pointer(&folderIDLocalAppData)), 0, 0, uintptr(unsafe.Pointer(&path)))
	// 失敗しても返された領域は解放する（SHGetKnownFolderPath の約束）。defer の引数は defer の
	// 時点で評価されるので、Call の後の path を渡すために関数で包む。
	defer func() { procCoTaskMemFree.Call(uintptr(unsafe.Pointer(path))) }()
	if int32(hr) < 0 || path == nil {
		return "", fmt.Errorf("SHGetKnownFolderPath(FOLDERID_LocalAppData) failed: 0x%08x", uint32(hr))
	}
	return utf16PtrToString(path), nil
}

// utf16PtrToString は NUL で終わる UTF-16 の列を文字列にする。
func utf16PtrToString(p *uint16) string {
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
