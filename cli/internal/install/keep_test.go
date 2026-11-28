package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// 新しい版を残置した install（フックの間に Vectorworks が起動した・フックが成功したにもかかわらず
// 記録を書き換えられなかった）と、それを同じタグの install で完了する流れ
// （docs/plugin/install-and-update.md）。

// failRecordWrite は、dir の記録の書き換えを失敗させる（記録は隣に書き込んでから置換するため、隣の
// ファイルを書き込めなくする）。返す関数で途中でも元に戻せる。
func failRecordWrite(t *testing.T, dir string) (restore func()) {
	t.Helper()
	prefix := filepath.Join(dir, RecordFile) + ".new-"
	return writeFileWhen(t, func(path string) error {
		if strings.HasPrefix(path, prefix) {
			return errInjected
		}
		return nil
	})
}

// checkMovedCLI は、退避した CLI の移動先を検証する。old に残存した退避した CLI が moved 個で、
// 残存したならば（復元できなかった）通知した文言 message が以前の版のフォルダ dir の中の復元先を
// 示し、0 個ならば dir に c1 の vw2026 が復元されている。
func checkMovedCLI(t *testing.T, h *harness, dir string, moved int, message string) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(h.work, "old", oldCLIName+"*")); len(left) != moved {
		t.Fatalf("%d moved "+edition.Command+" should stay in old: %v", moved, left)
	}
	if moved > 0 {
		if target := filepath.Join(dir, "bin", edition.Command); !strings.Contains(message, target) {
			t.Fatalf("the message should name where to put "+edition.Command+" back (%s): %s", target, message)
		}
		return
	}
	if data, err := os.ReadFile(filepath.Join(dir, "bin", edition.Command)); err != nil || string(data) != "cli c1" {
		t.Fatal("the previous version should have its "+edition.Command+" back:", string(data), err)
	}
}

// keepNewVersion は、c1 をインストールしてから c2 を残置した状態（c1 は old/cli）にする
// （keepVersion）。
func keepNewVersion(t *testing.T, h *harness) {
	t.Helper()
	h.mustInstall(release{name: "cli", tag: "c1"})
	keepVersion(t, h, "c2")
}

// keepVersion は、tag の install のフックの間に Vectorworks を起動させ（ロックを取得し）、tag を
// 残置した状態にする。終了したらロックを解放する（Vectorworks を終了した）。残置した版のフックは、
// 記録を追記し成功するものにしておく。
func keepVersion(t *testing.T, h *harness, tag string) {
	t.Helper()
	h.publish(release{name: "cli", tag: tag, install: h.lockingHook(hookPlan{Exit: 1})})
	done, unlock := h.lockWhenHookStarts(h.cliSpool())
	_, err := h.install("cli", tag)
	<-done
	if err == nil || !strings.Contains(err.Error(), "Vectorworks started") || h.installedVersion("cli") != tag {
		t.Fatal(err)
	}
	if record, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || !record.HookPending {
		t.Fatal("the kept version should be marked as hook_pending:", record, err)
	}
	unlock()
	hook := filepath.Join(h.plugins, "cli", "hooks", "install.hook")
	os.WriteFile(hook, []byte(planJSON(t, hookPlan{Stdout: "{}", Record: h.runs("cli")})), 0o644)
}

// 新しい版を残置した install を同じタグで完了する回は、old に残存した以前の版のタグを以前の
// タグとしてフックに渡す。
func TestFinishingInstallPassesThePreviousTag(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	last := runs[len(runs)-1]
	if last.Env[edition.EnvPrefix+"HOOK_PREVIOUS_TAG"] != "c1" || last.Env[edition.EnvPrefix+"HOOK_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
	if exists(filepath.Join(h.work, "old", "cli")) {
		t.Fatal("left the previous version")
	}
	if record, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || record.HookPending {
		t.Fatal("the hook succeeded, so hook_pending should be gone:", record, err)
	}
}

// 初回にインストールした版を残置した後、同じタグで完了する回は、以前のタグを空にする（初回の
// インストールと同じ）。
func TestFinishingFirstInstallPassesNoPreviousTag(t *testing.T) {
	h := newHarness(t)
	keepVersion(t, h, "c1")
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env[edition.EnvPrefix+"HOOK_PREVIOUS_TAG"] != "" || last.Env[edition.EnvPrefix+"HOOK_TAG"] != "c1" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックは成功したが、退避した版を整理できずに old に残存した場合は、インストールされている版が
// インストールを完了しているため、次の install はインストールされているタグを以前のタグにする
// （old の残存物から推測しない）。
func TestLeftoverOldAfterASuccessfulHookIsNotThePreviousTag(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	old := filepath.Join(h.work, "old", "cli")
	restore := renameWhen(t, func(from, to string) error {
		if from == old && to == filepath.Join(h.work, "staging", "cli") {
			return errInjected
		}
		return nil
	})
	result, err := h.install("cli", "c2")
	restore()
	if err != nil || result.Outcome != outcomeInstalled || !validRecord(old, "cli") {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env[edition.EnvPrefix+"HOOK_PREVIOUS_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
}

// インストールされている版のフックがまだ成功していなければ、--check は同じタグでも available
// （インストールを完了していない）。
func TestCheckAKeptVersion(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	if result, err := Install(h.options(), "cli", "c2", true); err != nil || result.Outcome != outcomeAvailable {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := Install(h.options(), "cli", "c2", true); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("after finishing: %+v %v", result, err)
	}
}

// フックの成功していない版を残置し、以前の版が old にある場合は、別のタグへ更新しない（old を
// 削除してから新しい版のフックが失敗すると、成功していない版のみが残存するため）。同じタグで
// インストールを完了すれば更新でき、以前のタグはインストールを完了した版。
func TestUpdatingAKeptVersionNeedsFinishing(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	h.publish(release{name: "cli", tag: "c3"})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c3"); err == nil || !strings.Contains(err.Error(), "has not finished installing") {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if !validRecord(filepath.Join(h.work, "old", "cli"), "cli") {
		t.Fatal("removed the previous version")
	}
	// --check は拒否せず、新しいビルドがあると応答する（help に、インストールを完了するまで
	// install が拒否すると記述する）。
	if result, err := Install(h.options(), "cli", "c3", true); err != nil || result.Outcome != outcomeAvailable {
		t.Fatalf("--check: %+v %v", result, err)
	}
	// Vectorworks が実行中であれば、拒否する理由より先に vectorworks_running と応答する
	// （終了コード 6）。
	release := h.holdLock()
	if result, err := h.install("cli", "c3"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning {
		t.Fatalf("running: %+v %v", result, err)
	}
	release()
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if result, err := h.install("cli", "c3"); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env[edition.EnvPrefix+"HOOK_PREVIOUS_TAG"] != "c2" || last.Env[edition.EnvPrefix+"HOOK_TAG"] != "c3" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックの成功していない版の module が欠落すると（隔離された）、同じタグでも再インストールとなる。
// 差し替えの前の後処理が old（フックの成功した最後の版）を削除しないよう、何も変更せずに拒否し、
// 以前の版を復元するか削除するかを示す（install と uninstall が互いを指し合わない）。示した通りに
// 以前の版を復元すれば、通常通り再インストールできる。
func TestReinstallingAKeptVersionWithoutItsModule(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	dest := filepath.Join(h.plugins, "cli")
	old := filepath.Join(h.work, "old", "cli")
	os.RemoveAll(filepath.Join(dest, "cli.vwlibrary"))
	h.publish(release{name: "cli", tag: "c2"})
	before := snapshot(t, h.plugins)
	if _, err := h.install("cli", "c2"); err == nil || !strings.Contains(err.Error(), "move it back there") ||
		!strings.Contains(err.Error(), old) {
		t.Fatal(err)
	}
	sameSnapshot(t, before, snapshot(t, h.plugins))
	if !validRecord(old, "cli") {
		t.Fatal("removed the previous version")
	}
	os.RemoveAll(dest)
	os.Rename(old, dest)
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeInstalled {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env[edition.EnvPrefix+"HOOK_PREVIOUS_TAG"] != "c1" || last.Env[edition.EnvPrefix+"HOOK_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックが成功した後に hook_pending を除去できなくても、インストールしたことは変わらず、
// 通知するのみとする。
func TestCannotClearHookPending(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	restore := failRecordWrite(t, filepath.Join(h.plugins, "cli"))
	o, stderr := h.capturing()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeUpToDate ||
		!strings.Contains(stderr.String(), "hook succeeded") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	// 記録は破損させない（書き換えが途中で停止しても、元の記録が残存する）。記録はまだ
	// 成功していないと示すため、退避した版も整理しない（以前のタグを失わない）。次の同じタグの
	// install が除去して整理する。
	if got, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || !got.HookPending {
		t.Fatal("the record should stay as it was:", got, err)
	}
	old := filepath.Join(h.work, "old", "cli")
	if !validRecord(old, "cli") {
		t.Fatal("removed the previous version while the record still says hook_pending")
	}
	restore()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeUpToDate || exists(old) {
		t.Fatalf("%+v %v", result, err)
	}
}

// フックが記録を破損させても（ここでは記録に追記する）、hook_pending を除去できないと
// 通知するのみで、インストールしたことは変わらない。
func TestHookBreaksTheRecord(t *testing.T) {
	h := newHarness(t)
	record := filepath.Join(h.plugins, "cli", RecordFile)
	h.publish(release{name: "cli", tag: "c1", install: &hookPlan{Stdout: "{}", Record: record}})
	o, stderr := h.capturing()
	if result, err := Install(o, "cli", "c1", false); err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "hook succeeded") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
}

// フックが成功したにもかかわらず hook_pending を除去できず以前の版を残置する場合は、退避した CLI を
// 以前の版のフォルダへ復元する（後処理が、以前の版の vw2026 を退避した名前のまま削除しない）。
// 復元できなければ、通知した場所のまま削除しない（後処理が削除すると、通知した場所に存在しない）。
// いずれのケースも、標準エラーの文言（共通の文言と、ケースごとに追加する extra）・old に残存した
// 退避した CLI の数・以前の版の vw2026 と復元先の案内を同じ形で検証する。
func TestCannotClearHookPendingPutsTheMovedCLIBack(t *testing.T) {
	for _, c := range []struct {
		name    string
		blocked bool
		extra   string
		moved   int
	}{
		{"put back", false, "", 0},
		{"cannot put back", true, "the " + edition.Command + " of the previous version is in", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			failRecordWrite(t, filepath.Join(h.plugins, "cli"))
			if c.blocked {
				failRename(t, "", filepath.Join(h.work, "old", "cli", "bin", edition.Command))
			}
			o, stderr := h.capturing()
			o.Executable = h.fakeExecutable()
			if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
				!strings.Contains(stderr.String(), "hook succeeded") ||
				!strings.Contains(stderr.String(), c.extra) {
				t.Fatalf("%+v %v %s", result, err, stderr.String())
			}
			checkMovedCLI(t, h, filepath.Join(h.work, "old", "cli"), c.moved, stderr.String())
		})
	}
}

// 新しい版を残置する場合、退避した CLI は以前の版のフォルダ（old）の元の場所へ復元する（以前の版を
// 手動で復元しても CLI が欠落せず、後処理が退避した名前のまま削除しない）。復元できなければ、その
// 場所を通知する。いずれのケースも、誤りの文言（共通の文言と、ケースごとに追加する extra）・old に
// 残存した退避した CLI の数・以前の版の vw2026 と復元先の案内を同じ形で検証する。
func TestKeepPutsTheMovedCLIIntoThePreviousVersion(t *testing.T) {
	for _, c := range []struct {
		name    string
		blocked bool
		extra   string
		moved   int
	}{
		{"put back", false, "", 0},
		{"cannot put back", true, "the " + edition.Command + " of the previous version is in", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2", install: h.lockingHook(hookPlan{Exit: 1})})
			if c.blocked {
				failRename(t, "", filepath.Join(h.work, "old", "cli", "bin", edition.Command))
			}
			done, _ := h.lockWhenHookStarts(h.cliSpool())
			o := h.options()
			o.Executable = h.fakeExecutable()
			_, err := Install(o, "cli", "c2", false)
			<-done
			if err == nil || !strings.Contains(err.Error(), "Vectorworks started") ||
				!strings.Contains(err.Error(), c.extra) {
				t.Fatal(err)
			}
			checkMovedCLI(t, h, filepath.Join(h.work, "old", "cli"), c.moved, err.Error())
		})
	}
}

// 記録の無い以前の版を .kept-<時刻> へ移動した場合は、退避した CLI をそこへ復元する（利用者が
// 復元する可能性のある版から、vw2026 を削除しない）。復元できなければ、その場所を通知し、退避した
// CLI を削除しない。いずれのケースも、標準エラーの文言（共通の文言と、ケースごとに追加する
// extra）・別の場所へ移動した版・old に残存した退避した CLI の数・別の場所へ移動した版の vw2026 と
// 復元先の案内を同じ形で検証する。
func TestKeptPreviousVersionGetsTheMovedCLI(t *testing.T) {
	for _, c := range []struct {
		name    string
		blocked bool
		extra   string
		moved   int
	}{
		{"put back", false, "", 0},
		{"cannot put back", true, "the " + edition.Command + " of the previous version is in", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			// module が欠落した（隔離された）インストール済みの cli から更新する。以前の版は記録が
			// 正しくないため、削除せずに別の場所へ移動する。
			os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
			if c.blocked {
				renameWhen(t, func(oldpath, newpath string) error {
					if strings.Contains(newpath, ".kept-") && strings.HasSuffix(newpath, filepath.Join("bin", edition.Command)) {
						return errInjected
					}
					return nil
				})
			}
			o, stderr := h.capturing()
			o.Executable = h.fakeExecutable()
			if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
				!strings.Contains(stderr.String(), "so it is kept in") ||
				!strings.Contains(stderr.String(), c.extra) {
				t.Fatalf("%+v %v %s", result, err, stderr.String())
			}
			kept, _ := filepath.Glob(filepath.Join(h.work, "old", "cli.kept-*"))
			if len(kept) != 1 {
				t.Fatal("kept:", kept, stderr.String())
			}
			checkMovedCLI(t, h, kept[0], c.moved, stderr.String())
		})
	}
}

// この回が CLI を退避していなければ、以前の版を別の場所へ移動しても、以前の回までの退避した CLI の
// 削除を試みる。
func TestKeptPreviousVersionWithoutAMovedCLICleansUp(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	// 記録の無い前回の残存物（同じタグの install が別の場所へ移動する）と、以前の回までの退避した
	// CLI。
	old := filepath.Join(h.work, "old")
	os.MkdirAll(filepath.Join(old, "cli"), 0o700)
	leftover := filepath.Join(old, oldCLIName+"20260101T000000.000000000")
	os.WriteFile(leftover, nil, 0o700)
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	if kept, _ := filepath.Glob(filepath.Join(old, "cli.kept-*")); len(kept) != 1 || exists(leftover) {
		t.Fatal(kept, exists(leftover))
	}
}

// 以前の版を Vectorworks が使用している可能性があるため残置する場合の案内は、呼び出す場面によって
// 異なる。差し替えの前ならば何も変更していないため再実行するのみ、フックの後ならば新しい版が
// インストールされているため、インストールされたタグで再実行して整理する。
func TestSettleOldAdviceDependsOnTheStage(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	// 差し替えの前: 前回の残存物の old/cli があり、マニフェストを取得する間に Vectorworks が
	// 起動する（後処理の検証で検出する）。
	old := filepath.Join(h.work, "old", "cli")
	if err := os.CopyFS(old, os.DirFS(filepath.Join(h.plugins, "cli"))); err != nil {
		t.Fatal(err)
	}
	h.publish(release{name: "cli", tag: "c2"})
	unlock := func() {}
	h.onGet("/cli/c2/"+edition.Command+".json", func() { unlock = h.holdLock() })
	if result, err := h.install("cli", "c2"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning ||
		!strings.Contains(err.Error(), "nothing is changed") || strings.Contains(err.Error(), "clean it up") {
		t.Fatalf("%+v %v", result, err)
	}
	unlock()
	os.RemoveAll(old)

	// フックの後: フックの間に Vectorworks が起動し、フックは成功する。
	h.publish(release{name: "cli", tag: "c3", install: h.lockingHook(hookPlan{})})
	done, _ := h.lockWhenHookStarts(h.cliSpool())
	o, stderr := h.capturing()
	result, err := Install(o, "cli", "c3", false)
	<-done
	if err != nil || result.Outcome != outcomeInstalled ||
		!strings.Contains(stderr.String(), "the new version is installed; quit Vectorworks, then run install cli@c3 again") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
}
