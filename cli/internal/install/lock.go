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
// <Plug-Ins>/cli/ が無く <Work>/old/cli/ があれば、cli の install・uninstall が途中で
// 止まっている。<Plug-Ins> を間違えたと読ませないよう、その続け方を答える
// （settleOld が cli のときに答えるのと同じ続け方）。
func checkCLI(p places, name string) error {
	if name == CLIName {
		return nil
	}
	dir := filepath.Join(p.plugins, CLIName)
	if validRecord(dir, CLIName) {
		return nil
	}
	oldDir := filepath.Join(p.old, CLIName)
	switch {
	case exists(dir) || !exists(oldDir):
	case exists(filepath.Join(p.old, CLIName+".uninstalling")):
		return fmt.Errorf("%s has no cli, as an uninstall of cli stopped halfway and left %s; "+
			"run uninstall cli again to finish it", p.plugins, oldDir)
	default:
		return fmt.Errorf("%s has no cli, as an install or uninstall of cli stopped halfway "+
			"and left %s (it may be the only copy of cli); move it back to %s, then run again",
			p.plugins, oldDir, dir)
	}
	return fmt.Errorf("%s has no cli that vw2026 installed; vw2026 installs and uninstalls a plug-in only in the "+
		"folder of plug-ins that has cli (it tells whether Vectorworks is running from the lock in %s)",
		p.plugins, filepath.Join(dir, spool.SpoolDirName))
}

// precheck は、install・uninstall が何かを変える前の確かめを、両方で同じ順にする。cli の記録（checkCLI）を、
// Vectorworks が動いているか（stopped）より先に確かめる（cli の記録の誤りは、Vectorworks を終えて走らせ直しても
// 通らないので、先に答える）。running が偽なら（install --check）動いているかは見ない。
func precheck(p places, name string, running bool) error {
	if err := checkCLI(p, name); err != nil {
		return err
	}
	if running {
		return stopped(p)
	}
	return nil
}
