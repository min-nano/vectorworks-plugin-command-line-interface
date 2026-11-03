package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 新しい版を残した install（フックの間に Vectorworks が起動した・フックが通ったのに記録を書き換えられなかった）と、
// それを同じタグの install で終える流れ（docs/plugin/install-and-update.md）。

// failRecordWrite は、dir の記録の書き換えを失敗させる（記録は隣に書いてから置き換えるので、隣のファイルを
// 書けなくする）。返す関数で途中でも元に戻せる。
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

// checkMovedCLI は、よけた CLI の行き先を確かめる。old に残ったよけた CLI が moved 個で、0 個なら前の版のフォルダ
// dir に c1 の vw2026 が戻っている。
func checkMovedCLI(t *testing.T, h *harness, dir string, moved int) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(h.work, "old", oldCLIName+"*")); len(left) != moved {
		t.Fatalf("%d moved vw2026 should stay in old: %v", moved, left)
	}
	if moved > 0 {
		return
	}
	if data, err := os.ReadFile(filepath.Join(dir, "bin", "vw2026")); err != nil || string(data) != "cli c1" {
		t.Fatal("the previous version should have its vw2026 back:", string(data), err)
	}
}

// keepNewVersion は、c1 を入れてから c2 を残した状態（c1 は old/cli）にする（keepVersion）。
func keepNewVersion(t *testing.T, h *harness) {
	t.Helper()
	h.mustInstall(release{name: "cli", tag: "c1"})
	keepVersion(t, h, "c2")
}

// keepVersion は、tag の install のフックの間に Vectorworks を起動させ（ロックを掴み）、tag を残した状態にする。
// 終わったらロックを放す（Vectorworks を終了した）。残した版のフックは、記録を書き足す通るものにしておく。
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

// 新しい版を残した install を同じタグで終える回は、old に残った前の版のタグを前のタグとしてフックに渡す。
func TestFinishingInstallPassesThePreviousTag(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	if result, err := h.install("cli", "c2"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	last := runs[len(runs)-1]
	if last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c1" || last.Env["VW2026_HOOK_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
	if exists(filepath.Join(h.work, "old", "cli")) {
		t.Fatal("left the previous version")
	}
	if record, err := readRecord(filepath.Join(h.plugins, "cli")); err != nil || record.HookPending {
		t.Fatal("the hook succeeded, so hook_pending should be gone:", record, err)
	}
}

// 最初に入れた版を残したあと、同じタグで終える回は、前のタグを空にする（最初に入れるのと同じ）。
func TestFinishingFirstInstallPassesNoPreviousTag(t *testing.T) {
	h := newHarness(t)
	keepVersion(t, h, "c1")
	if result, err := h.install("cli", "c1"); err != nil || result.Outcome != outcomeUpToDate {
		t.Fatalf("%+v %v", result, err)
	}
	runs := h.hookRuns("cli")
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "" || last.Env["VW2026_HOOK_TAG"] != "c1" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックは通ったが、退避した版を片付けられずに old に残ったときは、入っている版が入れ終えているので、次の
// install は入っているタグを前のタグにする（old の残りから推し量らない）。
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
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
}

// 入っている版のフックがまだ通っていなければ、--check は同じタグでも available（入れ終えていない）。
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

// フックの通っていない版を残し、前の版が old にあるときは、別のタグへ更新しない（old を消してから新しい版の
// フックが失敗すると、通っていない版だけが残るため）。同じタグで入れ終えれば更新でき、前のタグは入れ終えた版。
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
	// --check は断らず、新しいビルドがあると答える（help に、入れ終えるまで install が断ると書く）。
	if result, err := Install(h.options(), "cli", "c3", true); err != nil || result.Outcome != outcomeAvailable {
		t.Fatalf("--check: %+v %v", result, err)
	}
	// Vectorworks が動いていれば、断る理由より先に vectorworks_running と答える（終了コード 6）。
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
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c2" || last.Env["VW2026_HOOK_TAG"] != "c3" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックの通っていない版の module が欠けると（隔離された）、同じタグでも入れ直しになる。付け替えの前の片付けが
// old（フックの通った最後の版）を消さないよう、何も変えずに断り、前の版を戻すか消すかを示す（install と
// uninstall が互いを指し合わない）。示したとおり前の版を戻せば、ふつうに入れ直せる。
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
	if last := runs[len(runs)-1]; last.Env["VW2026_HOOK_PREVIOUS_TAG"] != "c1" || last.Env["VW2026_HOOK_TAG"] != "c2" {
		t.Fatalf("%+v", last.Env)
	}
}

// フックが通ったあとに hook_pending を外せなくても、入れたことは変わらず、知らせるだけにする。
func TestCannotClearHookPending(t *testing.T) {
	h := newHarness(t)
	keepNewVersion(t, h)
	restore := failRecordWrite(t, filepath.Join(h.plugins, "cli"))
	o, stderr := h.capturing()
	if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeUpToDate ||
		!strings.Contains(stderr.String(), "hook succeeded") {
		t.Fatalf("%+v %v %s", result, err, stderr.String())
	}
	// 記録は壊さない（書き換えが途中で止まっても、元の記録が残る）。記録はまだ通っていないと言うので、退避した
	// 版も片付けない（前のタグを失わない）。次の同じタグの install が外して片付ける。
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

// フックが記録を壊しても（ここでは記録に書き足す）、hook_pending を外せないと知らせるだけで、入れたことは変わらない。
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

// フックが通ったのに hook_pending を外せず前の版を残すときは、よけた CLI を前の版のフォルダへ戻す（後始末が、
// 前の版の vw2026 をよけた名前のまま消さない）。戻せなければ、知らせた場所のまま消さない（後始末が消すと、知らせた
// 場所に無い）。どのケースも、標準エラーの文言・old に残ったよけた CLI の数・前の版の vw2026 を同じ形で確かめる。
func TestCannotClearHookPendingPutsTheMovedCLIBack(t *testing.T) {
	for _, c := range []struct {
		name    string
		blocked bool
		stderr  string
		moved   int
	}{
		{"put back", false, "hook succeeded", 0},
		{"cannot put back", true, "the vw2026 of the previous version is in", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			failRecordWrite(t, filepath.Join(h.plugins, "cli"))
			if c.blocked {
				failRename(t, "", filepath.Join(h.work, "old", "cli", "bin", "vw2026"))
			}
			o, stderr := h.capturing()
			o.Executable = h.fakeExecutable()
			if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
				!strings.Contains(stderr.String(), "hook succeeded") || !strings.Contains(stderr.String(), c.stderr) {
				t.Fatalf("%+v %v %s", result, err, stderr.String())
			}
			checkMovedCLI(t, h, filepath.Join(h.work, "old", "cli"), c.moved)
		})
	}
}

// 新しい版を残すとき、よけた CLI は前の版のフォルダ（old）の元の場所へ戻す（前の版を手で戻しても CLI が欠けず、
// 後始末がよけた名前のまま消さない）。戻せなければ、その場所を知らせる。どのケースも、誤りの文言・old に残った
// よけた CLI の数・前の版の vw2026 を同じ形で確かめる。
func TestKeepPutsTheMovedCLIIntoThePreviousVersion(t *testing.T) {
	for _, c := range []struct {
		name    string
		blocked bool
		err     string
		moved   int
	}{
		{"put back", false, "Vectorworks started", 0},
		{"cannot put back", true, "the vw2026 of the previous version is in", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2", install: h.lockingHook(hookPlan{Exit: 1})})
			if c.blocked {
				failRename(t, "", filepath.Join(h.work, "old", "cli", "bin", "vw2026"))
			}
			done, _ := h.lockWhenHookStarts(h.cliSpool())
			o := h.options()
			o.Executable = h.fakeExecutable()
			_, err := Install(o, "cli", "c2", false)
			<-done
			if err == nil || !strings.Contains(err.Error(), "Vectorworks started") || !strings.Contains(err.Error(), c.err) {
				t.Fatal(err)
			}
			checkMovedCLI(t, h, filepath.Join(h.work, "old", "cli"), c.moved)
		})
	}
}

// 記録の無い前の版を .kept-<時刻> へ脇へ移したときは、よけた CLI をそこへ戻す（利用者が戻すかもしれない版から、
// vw2026 を消さない）。戻せなければ、その場所を知らせ、よけた CLI を消さない。どのケースも、標準エラーの文言・
// 脇へ移した版・old に残ったよけた CLI の数・脇へ移した版の vw2026 を同じ形で確かめる。
func TestKeptPreviousVersionGetsTheMovedCLI(t *testing.T) {
	for _, c := range []struct {
		name    string
		blocked bool
		stderr  string
		moved   int
	}{
		{"put back", false, "so it is kept in", 0},
		{"cannot put back", true, "the vw2026 of the previous version is in", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustInstall(release{name: "cli", tag: "c1"})
			h.publish(release{name: "cli", tag: "c2"})
			// module が欠けた（隔離された）入った cli から更新する。前の版は記録が正しくないので、消さずに脇へ移す。
			os.RemoveAll(filepath.Join(h.plugins, "cli", "cli.vwlibrary"))
			if c.blocked {
				renameWhen(t, func(oldpath, newpath string) error {
					if strings.Contains(newpath, ".kept-") && strings.HasSuffix(newpath, filepath.Join("bin", "vw2026")) {
						return errInjected
					}
					return nil
				})
			}
			o, stderr := h.capturing()
			o.Executable = h.fakeExecutable()
			if result, err := Install(o, "cli", "c2", false); err != nil || result.Outcome != outcomeInstalled ||
				!strings.Contains(stderr.String(), "so it is kept in") || !strings.Contains(stderr.String(), c.stderr) {
				t.Fatalf("%+v %v %s", result, err, stderr.String())
			}
			kept, _ := filepath.Glob(filepath.Join(h.work, "old", "cli.kept-*"))
			if len(kept) != 1 {
				t.Fatal("kept:", kept, stderr.String())
			}
			checkMovedCLI(t, h, kept[0], c.moved)
		})
	}
}

// この回が CLI をよけていなければ、前の版を脇へ移しても、前の回までのよけた CLI は消してみる。
func TestKeptPreviousVersionWithoutAMovedCLICleansUp(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	h.publish(release{name: "cli", tag: "c2"})
	// 記録の無い前回の残り（同じタグの install が脇へ移す）と、前の回までのよけた CLI。
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

// 前の版を Vectorworks が使っているかもしれないので残すときの案内は、呼ぶ場面で変わる。付け替えの前なら何も変えて
// いないので走らせ直すだけ、フックのあとなら新しい版が入っているので、入ったタグで走らせ直して片付ける。
func TestSettleOldAdviceDependsOnTheStage(t *testing.T) {
	h := newHarness(t)
	h.mustInstall(release{name: "cli", tag: "c1"})
	// 付け替えの前: 前回の残りの old/cli があり、マニフェストを取る間に Vectorworks が起動する（片付けの確かめで見つける）。
	old := filepath.Join(h.work, "old", "cli")
	if err := os.CopyFS(old, os.DirFS(filepath.Join(h.plugins, "cli"))); err != nil {
		t.Fatal(err)
	}
	h.publish(release{name: "cli", tag: "c2"})
	unlock := func() {}
	h.onGet("/cli/c2/vw2026.json", func() { unlock = h.holdLock() })
	if result, err := h.install("cli", "c2"); !errors.Is(err, ErrRunning) || result.Outcome != outcomeRunning ||
		!strings.Contains(err.Error(), "nothing is changed") || strings.Contains(err.Error(), "clean it up") {
		t.Fatalf("%+v %v", result, err)
	}
	unlock()
	os.RemoveAll(old)

	// フックのあと: フックの間に Vectorworks が起動し、フックは通る。
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
