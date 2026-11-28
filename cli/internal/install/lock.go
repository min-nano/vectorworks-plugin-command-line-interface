package install

import (
	"fmt"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// lockState は、Vectorworks が存在するか（docs/protocol.md「生存の判定」）。
// <Plug-Ins>/cli/spool/ と、<Work>/old/cli/spool/ の両方のロックを確認する。cli の
// 差し替え・uninstall の直前に前の版を読み込んだ Vectorworks のロックは、フォルダと共に
// old/cli/spool/ へ移動する（mac）ため、install・uninstall のいずれの確認もこれのみを使用し、
// 経路ごとに確認する場所を変更しない。いずれかのロックを、存在しないこと以外の理由で
// 調査できなければ（権限・破損した場所）、動作しているかが不明であるという誤りも返す
// （判定そのもの、spool.Open の意味は変更しない）。
func lockState(p places) (bool, error) {
	var unknown error
	for _, dir := range []string{filepath.Join(p.plugins, cliName), filepath.Join(p.old, cliName)} {
		held, err := spool.LockHeld(filepath.Join(dir, spool.SpoolDirName))
		if held {
			return true, nil
		}
		if err != nil && unknown == nil {
			unknown = fmt.Errorf("cannot tell whether Vectorworks is running, as the lock in %s cannot be read (%v); "+
				"nothing is changed", dir, err)
		}
	}
	return false, unknown
}

// stopped は、何かを変更する前の確認（開始時・同じタグのフックの前・差し替えの前・前の版を整理する
// 前）。Vectorworks が存在すれば ErrRunning（終了コード 6）、ロックを調査できなければその誤り
// （終了コード 1）を返す。フックが通過した後に前の版を整理する前（finishInstall の settleOld）にも
// 使用するが、そこで返った誤りは通知するのみで、install の結果は変更しない（新しい版は
// インストールされている）。
func stopped(p places) error {
	held, err := lockState(p)
	if held {
		return ErrRunning
	}
	return err
}

// running は、Vectorworks が存在するとみなすか。ロックを調査できない場合も存在するとみなす
// （差し替えやフックの後の確認で、動作しているかもしれない Vectorworks の下でフォルダを
// 移動しない）。何も変更していない場合は stopped を使用する。
func running(p places) bool {
	held, err := lockState(p)
	return held || err != nil
}

// startedMeanwhile は、フックの間（開始時の確認の後）に Vectorworks が起動したとみなした場合の文言
// （running が真。install と uninstall で同一に記述する）。
const startedMeanwhile = "Vectorworks started meanwhile (or its lock cannot be read)"

// checkCLI は、cli 以外をインストール・アンインストールする際に、<Plug-Ins>/cli/ に正しい記録が
// 存在するかを確認する。
// cli の無い <Plug-Ins> では Vectorworks が動作しているかを判定できないため。
// cli の install・uninstall が途中で停止して cli が存在しないこともあるため、Vectorworks を
// 終了してそれを完了するよう付記する（停止した状態は読み取らない。継続の方法は install cli /
// uninstall cli が応答する。CLAUDE.md の遵守事項 3）。
func checkCLI(plugins, name string) error {
	if name == cliName {
		return nil
	}
	dir := filepath.Join(plugins, cliName)
	if !validRecord(dir, cliName) {
		return fmt.Errorf("%s has no cli that "+edition.Command+" installed; "+edition.Command+" installs and "+
			"uninstalls a plug-in only in the folder of plug-ins that has cli (it tells "+
			"whether Vectorworks is running from the lock of cli). If an install or "+
			"uninstall of cli stopped halfway, quit Vectorworks, run that install or "+
			"uninstall of cli again with the first-time script ("+edition.Bootstrap+"; give it "+
			"--plugins-dir %s unless this is the default folder of plug-ins), then run "+
			"this again", plugins, plugins)
	}
	return nil
}

// precheck は、install・uninstall が何かを変更する前の確認を、両方で同じ順序とする。
// cli の記録（checkCLI）を、Vectorworks が動作しているか（stopped）より先に確認する（cli の記録の
// 誤りは、Vectorworks を終了して実行し直しても通過しないため、先に応答する）。cli の
// install・uninstall が途中で停止して cli が存在しない場合は、Vectorworks を終了して先にそれを
// 完了すれば通過する（checkCLI の文言がそのように案内する）。checkLock が偽ならば（install
// --check）動作しているかは確認しない。
func precheck(p places, name string, checkLock bool) error {
	if err := checkCLI(p.plugins, name); err != nil {
		return err
	}
	if checkLock {
		return stopped(p)
	}
	return nil
}
