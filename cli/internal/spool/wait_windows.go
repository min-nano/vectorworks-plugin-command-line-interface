//go:build windows

package spool

import "syscall"

// holdWait は <id>.wait を作って掴む（docs/protocol.md「待つ印」）。返す関数で放す。
//
// 掴み方は bridge.lock と同じく共有なし（share mode 0）で開いたハンドルを持ち続ける。
// ハンドルはプロセスが終われば（異常終了でも）OS が閉じる。lpSecurityAttributes に nil を
// 渡し、子プロセスに受け継がせない。
func holdWait(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.CREATE_NEW, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { syscall.CloseHandle(handle) }, nil
}
