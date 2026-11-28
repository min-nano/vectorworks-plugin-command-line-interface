package install

// syncDirectory は何もしない（読むだけで開いたディレクトリのハンドルは FlushFileBuffers で断られる。NTFS は
// メタデータをジャーナルに書くので、ファイルのデータを Sync してから名前を変えれば足りる。durable.go）。
func syncDirectory(string) error {
	return nil
}
