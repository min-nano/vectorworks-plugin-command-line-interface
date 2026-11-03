package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// replace は zip を取って組み立て、プラグインのフォルダを付け替えてフックを走らせる
// （docs/plugin/install-and-update.md「入れ替えの手順」）。失敗したら <Plug-Ins> を元に戻す。
func (o *Options) replace(p places, name, source, tag, previous string, asset registry.Asset) (json.RawMessage, error) {
	for _, dir := range []string{p.staging, p.old} {
		if err := mkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	// 前回の残りの old を片付けてから、組み立ての場所（名前をそのまま組み立てたパスだけ）を消す。
	// settleOld が消すものは組み立ての場所を通るので、この順にすると、消しきれなかったものがあれば
	// removeStaging が失敗し、前の版のファイルを新しい版に混ぜて組み立てない。
	if _, err := o.settleOld(p, name, "nothing is changed; quit Vectorworks, then run install again"); err != nil {
		return nil, err
	}
	if err := removeStaging(p); err != nil {
		return nil, err
	}

	// 組み立て。失敗したら組み立てたものを消す。
	record, err := o.assemble(p, name, source, tag, asset)
	if err != nil {
		_ = removeStaging(p)
		return nil, err
	}

	// 付け替えの直前にロックをもう一度見る（zip を取って組み立てる間に起動した Vectorworks を
	// 見落とさないため）。
	if err := stopped(p); err != nil {
		_ = removeStaging(p)
		return nil, err
	}
	moved, err := o.sidestep(p.dest, p.old)
	if err != nil {
		_ = removeStaging(p)
		return nil, err
	}
	hadOld := exists(p.dest)
	if hadOld {
		if err := rename(p.dest, p.oldDir); err != nil {
			note := moved.restore()
			_ = removeStaging(p)
			return nil, fmt.Errorf("%w%s", renameError(p.dest, err), note)
		}
	}
	if err := mkdirAll(p.plugins, 0o755); err != nil {
		return nil, o.undoSwap(p, hadOld, moved, err)
	}
	if err := rename(p.stageDir, p.dest); err != nil {
		return nil, o.undoSwap(p, hadOld, moved, err)
	}

	hook, err := o.runHook("install", record.Hooks.Install, p.dest, hookEnv{
		name: name, tag: tag, previous: previous, target: o.Target, pluginDir: p.dest, installDir: p.dest,
	})
	if err != nil {
		return nil, o.undoHook(p, hadOld, moved, err)
	}
	o.finishInstall(p, name, tag, moved, hadOld)
	return hook, nil
}

// finishInstall は install のフックが通ったあとの後始末（付け替えのあとも、同じタグでフックだけを走らせたあとも
// これだけを使う）。退避した版を settleOld の規則で片付け（守ること 9 の歯止め。片付けられなくても入れたことは
// 変わらないので、知らせるだけにする。次の install が片付ける）、組み立ての場所の残りと中断した uninstall の印を
// 消し（印を残すと、次の install が付け替えの途中で止まったときに、次の uninstall が唯一残った old/<name>/ を
// 消してしまう）、よけた CLI を消してみる（動いていて消せなければ残す）。
func (o *Options) finishInstall(p places, name, tag string, moved *movedCLI, hadOld bool) {
	// フックが通ったので、記録の hook_pending を外す。外せなければ知らせ、退避した版は片付けない（記録はまだ
	// 通っていないと言うので、次の同じタグの install が前の版のタグでフックを走らせ直し、そこで外して片付ける）。
	// 退避した版を残すときは、よけた CLI をその版のフォルダの元の場所へ戻す（undoHook の keep と同じ。利用者が前の版を
	// 手で戻したときに CLI が欠けず、下の後始末がよけた名前のまま消さないように）。
	// 記録の無い前の版を <Work>/old/<name>.kept-<時刻>/ へ脇へ移したときも、よけた CLI をそこへ戻す（利用者が戻す
	// かもしれない）。
	var kept error
	keptDir := ""
	if err := clearHookPending(p.dest); err != nil {
		kept = fmt.Errorf("cannot record that the install hook succeeded (%v); run install with the same tag again", err)
	} else {
		keptDir, kept = o.settleOld(p, name, fmt.Sprintf("the new version is installed; quit Vectorworks, then run "+
			"install %s@%s again to clean it up", name, tag))
	}
	if kept != nil && hadOld && exists(p.oldDir) {
		keptDir = p.oldDir
	}
	note := ""
	if hadOld && keptDir != "" {
		note = moved.restoreInto(p.dest, keptDir)
	}
	if kept != nil {
		fmt.Fprintf(o.stderr(), "vw2026: %v%s\n", kept, note)
	} else if note != "" {
		fmt.Fprintf(o.stderr(), "vw2026: the previous version is kept in %s%s\n", keptDir, note)
	}
	_ = removeStaging(p)
	_ = removeFile(p.marker)
	// この回がよけた CLI を前の版へ戻したとき（戻せなかったときは、手で戻す場所として知らせている。undoHook の keep
	// と同じ）は、よけた CLI を消さない。次の install・uninstall が消す。よけていなければ、前の回までの残りを消してみる。
	if moved == nil || keptDir == "" {
		cleanOldCLIs(p.old)
	}
}

// undoSwap は付け替えの 2 つ目が失敗したときに、1 つ目とよけた CLI を戻す。
func (o *Options) undoSwap(p places, hadOld bool, moved *movedCLI, cause error) error {
	if hadOld {
		if err := rename(p.oldDir, p.dest); err != nil {
			return fmt.Errorf("cannot put %s into %s (%v), nor put the previous version back (%v); "+
				"move %s back to %s by hand%s", p.stageDir, p.dest, cause, err, p.oldDir, p.dest, moved.where())
		}
	}
	note := moved.restore()
	_ = removeStaging(p)
	return fmt.Errorf("cannot put %s into %s: %w%s", p.stageDir, p.dest, cause, note)
}

// undoHook はフックが失敗したときに前の版へ戻す。戻す前にロックを見て、Vectorworks が起動していれば
// 新しい版を残す（動いているプラグインのロックごと消さない）。
func (o *Options) undoHook(p places, hadOld bool, moved *movedCLI, cause error) error {
	previous := ""
	if hadOld {
		previous = fmt.Sprintf(" (the previous one is in %s)", p.oldDir)
	}
	keep := func(reason string) error {
		// よけた CLI は、前の版のフォルダ（old）の元の場所へ戻す（利用者が前の版を手で戻したときに CLI が欠けず、
		// 後始末がよけた名前のまま消さないように）。戻せなければ、その場所を知らせる。
		note := ""
		if hadOld {
			note = moved.restoreInto(p.dest, p.oldDir)
		}
		// 取った zip はもう要らない（同じタグで走らせ直す install は zip を取らない）。
		_ = removeFile(p.stageZip)
		return fmt.Errorf("%w; %s, so the new version stays in %s%s: quit Vectorworks, "+
			"then run install with the same tag again (it runs only the hook)%s", cause, reason, p.dest, previous, note)
	}
	if running(p) {
		return keep(startedMeanwhile)
	}
	// 新しい版は名前の変更でよける（消すのはあと。削除が途中で止まると、半分消えたプラグインが
	// Plug-Ins に残るため）。
	if err := rename(p.dest, p.stageDir); err != nil {
		return keep(fmt.Sprintf("it cannot be moved away (%v; Vectorworks may have loaded it)", err))
	}
	if hadOld {
		if err := rename(p.oldDir, p.dest); err != nil {
			return fmt.Errorf("%w; the previous version cannot be put back (%v): move %s back to %s by hand%s",
				cause, err, p.oldDir, p.dest, moved.where())
		}
	}
	note := moved.restore()
	_ = removeStaging(p)
	if note != "" {
		return fmt.Errorf("%w; the previous version is put back%s", cause, note)
	}
	return fmt.Errorf("%w; the previous state is restored", cause)
}

// removeStaging は組み立ての場所（<Work>/staging/<name>/ と <name>.zip）を消す。
func removeStaging(p places) error {
	if err := removeAll(p.stageDir); err != nil {
		return err
	}
	if err := removeFile(p.stageZip); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// settleOld は <Work>/old/<name>/ を片付ける。install のどの道（付け替えの前・付け替えのあと・同じタグで
// フックだけを走らせたあと）もこれだけを使う（docs/plugin/install-and-update.md「<Work>/old/<name>/ の
// 片付け」）。
//
//   - <Plug-Ins>/<name>/ が無ければ、old が唯一の版かもしれないので、何も変えずに失敗にする（印があれば、
//     止まった uninstall の続きを示す）。
//   - 記録の名前が一致し module があれば、消す（守ること 9）。
//   - そうでなければ（module の無いフォルダを入れ直した前の版など。利用者のファイルが入っているかも
//     しれない）、消さずに <Work>/old/<name>.kept-<時刻>/ へ移して知らせる。old/<name>/ に置いたままに
//     すると、次の install・uninstall が通らないため。
//
// 脇へ移したときは、その場所を返す。
//
// advice は、Vectorworks が使っているかもしれないので残すときに添える、次にすること（呼ぶ側が決める。付け替えの前は
// 何も変えていない、付け替えのあとは新しい版が入っている）。
func (o *Options) settleOld(p places, name, advice string) (string, error) {
	switch {
	case !exists(p.oldDir):
		return "", nil
	case !exists(p.dest) && exists(p.marker):
		return "", fmt.Errorf("%s was left by an uninstall that stopped halfway; run uninstall %s again to finish it",
			p.oldDir, name)
	case !exists(p.dest):
		return "", fmt.Errorf("%s was left by an install or uninstall that stopped halfway; it may be the only copy of %s. "+
			"Move it back to %s or delete it, then run again", p.oldDir, name, p.dest)
	}
	// 動いている Vectorworks が使っているかもしれないものは、消しも移しもしない（フックの間に起動したとき。
	// 始めの確かめでは居なかった）。付け替えの前なら、install は ErrRunning（ロックを調べられなければその誤り）で
	// 何も変えずに終わる。
	if err := stopped(p); err != nil {
		return "", fmt.Errorf("%w; %s is left because Vectorworks may use it (%s)", err, p.oldDir, advice)
	}
	if validRecord(p.oldDir, name) {
		return "", o.discard(p, name)
	}
	kept := filepath.Join(p.old, name+".kept-"+stamp())
	if err := rename(p.oldDir, kept); err != nil {
		return "", renameError(p.oldDir, err)
	}
	fmt.Fprintf(o.stderr(), "vw2026: the previous folder of %s has no valid record or module, so it is kept in %s "+
		"(it may hold your files); delete it when it is no longer needed\n", name, kept)
	return kept, nil
}

// discard は、確かめた <Work>/old/<name>/ を組み立ての場所（<Work>/staging/<name>/）へ名前の変更で
// 移してから消す。消すのが途中で止まっても（Windows でウイルス対策がファイルを開いているなど）、半分
// 消えたフォルダは old に残らず（記録が先に消えて、次の回が唯一の版と見分けられなくなることがない）、
// 次の install・uninstall が組み立ての場所として消す。移せなければ何も変えずに失敗にする。
func (o *Options) discard(p places, name string) error {
	if err := removeStaging(p); err != nil {
		return err
	}
	// uninstall は空の <Work>/staging を消すので、どの道から来ても、ここで作る。
	if err := mkdirAll(p.staging, 0o700); err != nil {
		return err
	}
	if err := rename(p.oldDir, p.stageDir); err != nil {
		return renameError(p.oldDir, err)
	}
	if err := removeAll(p.stageDir); err != nil {
		fmt.Fprintf(o.stderr(), "vw2026: %s cannot be deleted yet (%v); the next install or uninstall of %s deletes it\n",
			p.stageDir, err, name)
	}
	return nil
}

// renameError は付け替えの 1 つ目（<Plug-Ins>/<name> を移す）が失敗したときの文言。Windows では、
// フォルダの中のものを開いているプロセスが居ると名前を変えられない。どれかは見分けられない。
func renameError(dir string, err error) error {
	return fmt.Errorf("cannot rename %s (%w). Close what has files in it open (Explorer, antivirus software, "+
		"Vectorworks), move the current directory of terminals and other programs out of it, then run again", dir, err)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
