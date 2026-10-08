//go:build windows

package spool

import (
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var procSetFileInformationByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle")

const (
	fileRenameInfoClass = 3          // FileRenameInfo
	accessDelete        = 0x00010000 // DELETE
)

// fileRenameInfo は FILE_RENAME_INFO と同じ並び。
type fileRenameInfo struct {
	Flags          uint32 // ReplaceIfExists。偽（既にあれば失敗）
	RootDirectory  syscall.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// claim は要求を確保する（docs/protocol.md「プラグイン側の義務」）。Windows の MoveFileEx は
// 元の名前で開いたハンドルを通して rename するので、間に相手が rename するとハンドルが移った
// ファイルを指したまま同じ名前への rename も成功し、2 つの橋が同じ要求を確保してしまう。
// 共有なしで開けば相手は開けない（共有違反）ので、そのハンドルで rename する。
func claim(from, to string) error {
	path, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(path, accessDelete, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)
	absolute, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	name := utf16.Encode([]rune(absolute))
	size := int(unsafe.Offsetof(fileRenameInfo{}.FileName)) + 2*(len(name)+1)
	if size < int(unsafe.Sizeof(fileRenameInfo{})) {
		size = int(unsafe.Sizeof(fileRenameInfo{}))
	}
	// uint64 の並びで確保して、構造体の境界に揃える。
	buffer := make([]uint64, (size+7)/8)
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.FileNameLength = uint32(2 * len(name))
	copy(unsafe.Slice(&info.FileName[0], len(name)+1), name)
	ok, _, err := procSetFileInformationByHandle.Call(uintptr(handle), fileRenameInfoClass, uintptr(unsafe.Pointer(info)), uintptr(size))
	if ok == 0 {
		return err
	}
	return nil
}
