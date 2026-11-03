package install

import (
	"fmt"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// lockState は、Vectorworks が居るか（docs/protocol.md「生存の判定」）。<Plug-Ins>/cli/spool/ と、
// <Work>/old/cli/spool/ の両方のロックを見る。cli の付け替え・uninstall の直前に前の版を読み込んだ
// Vectorworks のロックは、フォルダと一緒に old/cli/spool/ へ移る（mac）ので、install・uninstall のどの確かめも
// これだけを使い、道ごとに見る場所を変えない。どちらかのロックを無い以外の理由で調べられなければ（権限・壊れた
// 場所）、動いているかが分からないという誤りも返す（判定そのもの、spool.Open の意味は変えない）。
func lockState(p places) (bool, error) {
	var unknown error
	for _, dir := range []string{filepath.Join(p.plugins, CLIName), filepath.Join(p.old, CLIName)} {
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

// stopped は、何かを変える前の確かめ（始め・同じタグのフックの前・付け替えの前）。Vectorworks が居れば ErrRunning
// （終了コード 6）、ロックを調べられなければその誤り（終了コード 1）を返す。どちらも何も変えていないときに使う。
func stopped(p places) error {
	held, err := lockState(p)
	if held {
		return ErrRunning
	}
	return err
}

// running は、Vectorworks が居るとみなすか。ロックを調べられないときも居るとみなす（付け替えやフックのあとの
// 確かめで、動いているかもしれない Vectorworks の下でフォルダを動かさない）。何も変えていないときは stopped を使う。
func running(p places) bool {
	held, err := lockState(p)
	return held || err != nil
}

// startedMeanwhile は、フックの間（始めの確かめのあと）に Vectorworks が起動したとみなしたときの文言（running が
// 真。install と uninstall で同じに書く）。
const startedMeanwhile = "Vectorworks started meanwhile (or its lock cannot be read)"

// checkCLI は、cli 以外を入れる・外すときに、<Plug-Ins>/cli/ に正しい記録があるかを確かめる。
// cli の無い <Plug-Ins> では Vectorworks が動いているかを判定できないため。
// cli の install・uninstall が途中で止まって cli が無いこともあるので、Vectorworks を
// 終えてそれを終えるよう添える（止まった状態は読まない。続け方は install cli /
// uninstall cli が答える。CLAUDE.md の守ること 3）。
func checkCLI(plugins, name string) error {
	if name == CLIName {
		return nil
	}
	dir := filepath.Join(plugins, CLIName)
	if !validRecord(dir, CLIName) {
		return fmt.Errorf("%s has no cli that vw2026 installed; vw2026 installs and "+
			"uninstalls a plug-in only in the folder of plug-ins that has cli (it tells "+
			"whether Vectorworks is running from the lock of cli). If an install or "+
			"uninstall of cli stopped halfway, quit Vectorworks, run that install or "+
			"uninstall of cli again with the first-time script (get-vw2026; give it "+
			"--plugins-dir %s unless this is the default folder of plug-ins), then run "+
			"this again", plugins, plugins)
	}
	return nil
}

// precheck は、install・uninstall が何かを変える前の確かめを、両方で同じ順にする。cli の記録（checkCLI）を、
// Vectorworks が動いているか（stopped）より先に確かめる（cli の記録の誤りは、Vectorworks を終えて走らせ直しても
// 通らないので、先に答える）。cli の install・uninstall が途中で止まって cli が無いときは、
// Vectorworks を終えて先にそれを終えれば通る（checkCLI の文言がそう案内する）。checkLock が
// 偽なら（install --check）動いているかは見ない。
func precheck(p places, name string, checkLock bool) error {
	if err := checkCLI(p.plugins, name); err != nil {
		return err
	}
	if checkLock {
		return stopped(p)
	}
	return nil
}
