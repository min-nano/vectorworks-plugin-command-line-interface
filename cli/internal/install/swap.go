package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// replace は zip を取得して構築し、プラグインのフォルダを差し替えてフックを実行する
// （docs/plugin/install-and-update.md「置換の手順」）。失敗したら <Plug-Ins> を元に戻す。
func (o *Options) replace(p places, name, source, tag, previous string, asset registry.Asset) (json.RawMessage, error) {
	for _, dir := range []string{p.staging, p.old} {
		if err := mkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	// 前回の残存物の old を整理してから、構築の場所（名前をそのまま用いて構築したパスのみ）を
	// 削除する。settleOld が削除するものは構築の場所を経由するため、この順序とすると、
	// 削除しきれなかったものがあれば removeStaging が失敗し、前の版のファイルを新しい版に混入させて
	// 構築しない。
	again := installCommand(name, tag)
	stillOld := "nothing is changed; quit Vectorworks, then run " + again + " again"
	if _, err := o.settleOld(p, name, again, stillOld); err != nil {
		return nil, err
	}
	if err := removeStaging(p); err != nil {
		return nil, err
	}

	// 構築。失敗したら構築したものを削除する。
	record, err := o.assemble(p, name, source, tag, asset)
	if err != nil {
		_ = removeStaging(p)
		return nil, err
	}

	// 差し替えの直前にロックを再度確認する（zip を取得して構築する間に起動した Vectorworks を
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
			return nil, fmt.Errorf("%w%s", renameError(p.dest, err, again), note)
		}
	}
	if err := mkdirAll(p.plugins, 0o755); err != nil {
		return nil, undoSwap(p, hadOld, moved, err)
	}
	if err := rename(p.stageDir, p.dest); err != nil {
		return nil, undoSwap(p, hadOld, moved, err)
	}

	hook, err := o.runHook("install", record.Hooks.Install, hookEnv{
		name: name, tag: tag, previous: previous, target: o.Target, pluginDir: p.dest, installDir: p.dest,
	})
	if err != nil {
		return nil, undoHook(p, hadOld, moved, again, err)
	}
	o.finishInstall(p, name, tag, moved, hadOld)
	return hook, nil
}

// finishInstall は install のフックが通過した後の後処理（差し替えの後も、同じタグでフックのみを
// 実行した後もこれのみを使用する）。退避した版を settleOld の規則で整理し（遵守事項 9 の制約。
// 整理できなくてもインストールしたことは変わらないため、通知するのみとする。次の install が
// 整理する）、構築の場所の残存物と中断した uninstall の標識を削除し（標識を残置すると、
// 次の install が差し替えの途中で停止した場合に、次の uninstall が唯一残存した old/<name>/ を
// 削除してしまう）、退避した CLI の削除を試みる（実行中で削除できなければ残置する）。
func (o *Options) finishInstall(p places, name, tag string, moved *movedCLI, hadOld bool) {
	// フックが通過したため、記録の hook_pending を除去する。除去できなければ通知し、
	// 退避した版は整理しない（記録はまだ通過していないことを示すため、次の同じタグの install が前の
	// 版のタグでフックを再実行し、そこで除去して整理する）。
	// 退避した版を残置する場合は、退避した CLI をその版のフォルダの元の場所へ戻す（undoHook の
	// keep と同様。利用者が前の版を手作業で戻した場合に CLI が欠落せず、下の後処理が退避した
	// 名前のまま削除しないように）。
	// 記録の無い前の版を <Work>/old/<name>.kept-<時刻>/ へ移動して隔離した場合も、
	// 退避した CLI をそこへ戻す（利用者が戻すかもしれない）。
	var kept error
	keptDir := ""
	again := installCommand(name, tag)
	if err := clearHookPending(p.dest); err != nil {
		kept = fmt.Errorf("cannot record that the install hook succeeded (%v); run %s again",
			err, again)
	} else {
		cleanUp := "the new version is installed; quit Vectorworks, then run " + again +
			" again to clean it up"
		keptDir, kept = o.settleOld(p, name, again, cleanUp)
	}
	if kept != nil && hadOld && exists(p.oldDir) {
		keptDir = p.oldDir
	}
	note := ""
	if hadOld && keptDir != "" {
		note = moved.restoreInto(keptDir)
	}
	if kept != nil {
		fmt.Fprintf(o.stderr(), edition.Command+": %v%s\n", kept, note)
	} else if note != "" {
		fmt.Fprintf(o.stderr(), edition.Command+": the previous version is kept in %s%s\n", keptDir, note)
	}
	_ = removeStaging(p)
	_ = removeFile(p.marker)
	// この回が退避した CLI を前の版へ戻した場合（戻せなかった場合は、手作業で戻す場所として
	// 通知している。undoHook の keep と同様）は、退避した CLI を削除しない。次の
	// install・uninstall が削除する。退避していなければ、前の回までの残存物の削除を試みる。
	if moved == nil || keptDir == "" {
		cleanOldCLIs(p.old)
	}
}

// undoSwap は差し替えの 2 つ目が失敗した場合に、1 つ目と退避した CLI を戻す。
func undoSwap(p places, hadOld bool, moved *movedCLI, cause error) error {
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

// undoHook はフックが失敗した場合に前の版へ戻す。戻す前にロックを確認し、Vectorworks が
// 起動していれば新しい版を残置する（動作しているプラグインをロックごと削除しない）。again は、
// 再実行するコマンド（install <name>@<tag>）。
func undoHook(p places, hadOld bool, moved *movedCLI, again string, cause error) error {
	previous := ""
	if hadOld {
		previous = fmt.Sprintf(" (the previous one is in %s)", p.oldDir)
	}
	keep := func(reason string) error {
		// 退避した CLI は、前の版のフォルダ（old）の元の場所へ戻す（利用者が前の版を手作業で戻した
		// 場合に CLI が欠落せず、後処理が退避した名前のまま削除しないように）。戻せなければ、
		// その場所を通知する。
		note := ""
		if hadOld {
			note = moved.restoreInto(p.oldDir)
		}
		// 取得した zip はもう不要である（同じタグで再実行する install は zip を取得しない）。
		_ = removeFile(p.stageZip)
		return fmt.Errorf("%w; %s, so the new version stays in %s%s: quit Vectorworks, "+
			"then run %s again (it runs only the hook)%s",
			cause, reason, p.dest, previous, again, note)
	}
	if running(p) {
		return keep(startedMeanwhile)
	}
	// 新しい版は名前の変更で退避する（削除するのは後。削除が途中で停止すると、半分削除された
	// プラグインが Plug-Ins に残るため）。
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

// removeStaging は構築の場所（<Work>/staging/<name>/ と <name>.zip）を削除する。
func removeStaging(p places) error {
	if err := removeAll(p.stageDir); err != nil {
		return err
	}
	if err := removeFile(p.stageZip); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// settleOld は <Work>/old/<name>/ を整理する。install のいずれの経路（差し替えの前・差し替えの後・
// 同じタグでフックのみを実行した後）もこれのみを使用する
// （docs/plugin/install-and-update.md「<Work>/old/<name>/ の整理」）。
//
//   - <Plug-Ins>/<name>/ が無ければ、old が唯一の版かもしれないため、何も変更せずに失敗とする
//     （標識があれば、停止した uninstall の続きを示す）。
//   - 記録の名前が一致し module が存在すれば、削除する（遵守事項 9）。
//   - そうでなければ（module の無いフォルダを再インストールした前の版など。利用者のファイルが
//     格納されているかもしれない）、削除せずに <Work>/old/<name>.kept-<時刻>/ へ移動して通知する。
//     old/<name>/ に配置したままにすると、次の install・uninstall が通過しないため。
//
// 隔離のために移動した場合は、その場所を返す。
//
// again は、再実行するコマンド（install <name>@<tag>。整理できない場合の案内に使用する）。
// advice は、Vectorworks が使用しているかもしれないため残置する場合に付記する、
// 次に行うこと（呼び出し元が決定する。差し替えの前は何も変更していない、差し替えの後は新しい版が
// インストールされている）。
func (o *Options) settleOld(p places, name, again, advice string) (string, error) {
	switch {
	case !exists(p.oldDir):
		return "", nil
	case !exists(p.dest) && exists(p.marker):
		return "", fmt.Errorf("%s was left by an uninstall that stopped halfway; run uninstall %s again to finish it",
			p.oldDir, name)
	case !exists(p.dest):
		return "", fmt.Errorf("%s was left by an install or uninstall that stopped halfway; "+
			"it may be the only copy of %s. Move it back to %s or delete it, then run %s again",
			p.oldDir, name, p.dest, again)
	}
	// 動作している Vectorworks が使用しているかもしれないものは、削除も移動もしない（フックの間に
	// 起動した場合。開始時の確認では存在しなかった）。差し替えの前ならば、install は
	// ErrRunning（ロックを調査できなければその誤り）で何も変更せずに終了する。
	if err := stopped(p); err != nil {
		return "", fmt.Errorf("%w; %s is left because Vectorworks may use it (%s)", err, p.oldDir, advice)
	}
	if validRecord(p.oldDir, name) {
		return "", o.discard(p, name, again)
	}
	kept := filepath.Join(p.old, name+".kept-"+stamp())
	if err := rename(p.oldDir, kept); err != nil {
		return "", renameError(p.oldDir, err, again)
	}
	fmt.Fprintf(o.stderr(), edition.Command+": the previous folder of %s has no valid record or module, so it is kept in %s "+
		"(it may hold your files); delete it when it is no longer needed\n", name, kept)
	return kept, nil
}

// discard は、検証した <Work>/old/<name>/ を構築の場所（<Work>/staging/<name>/）へ名前の変更で
// 移動してから削除する。削除が途中で停止しても（Windows でウイルス対策がファイルを
// 開いているなど）、半分削除されたフォルダは old に残らず（記録が先に削除されて、
// 次の回が唯一の版と区別できなくなることがない）、次の install・uninstall が構築の場所として
// 削除する。移動できなければ何も変更せずに失敗とする。いずれの失敗も、command（再実行するコマンド。
// install <name>@<tag> か uninstall <name>）を再実行するよう案内する文言で返す（呼び出し元は案内を
// 追加しない）。
func (o *Options) discard(p places, name, command string) error {
	if err := removeStaging(p); err != nil {
		return fmt.Errorf("%w; run %s again", err, command)
	}
	// uninstall は空の <Work>/staging を削除するため、いずれの経路から来ても、ここで作成する。
	if err := mkdirAll(p.staging, 0o700); err != nil {
		return fmt.Errorf("%w; run %s again", err, command)
	}
	if err := rename(p.oldDir, p.stageDir); err != nil {
		return renameError(p.oldDir, err, command)
	}
	if err := removeAll(p.stageDir); err != nil {
		fmt.Fprintf(o.stderr(), edition.Command+": %s cannot be deleted yet (%v); the next install or uninstall of %s deletes it\n",
			p.stageDir, err, name)
	}
	return nil
}

// renameError は差し替えの 1 つ目（<Plug-Ins>/<name> を移動する）が失敗した場合の文言。
// Windows では、フォルダの中のものを開いているプロセスが存在すると名前を変更できない。
// いずれであるかは区別できない。command（再実行するコマンド。install <name>@<tag> か uninstall
// <name>）を再実行するよう案内する。
func renameError(dir string, err error, command string) error {
	return fmt.Errorf("cannot rename %s (%w). Close what has files in it open (Explorer, antivirus software, "+
		"Vectorworks), move the current directory of terminals and other programs out of it, then run %s again",
		dir, err, command)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
