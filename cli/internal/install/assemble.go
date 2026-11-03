package install

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// RecordFile は入れたときの記録（docs/registry.md「入れたときの記録」）。
const RecordFile = "vw2026-install.json"

// recordVersion は読み書きする record_version。
const recordVersion = 1

// Record は入れたときの記録。プラグインのフォルダの中にだけ置き、フォルダと一緒に付け替える。
type Record struct {
	RecordVersion int            `json:"record_version"`
	Name          string         `json:"name"`
	Registry      string         `json:"registry"`
	Tag           string         `json:"tag"`
	Target        string         `json:"target"`
	Module        string         `json:"module"`
	Hooks         registry.Hooks `json:"hooks"`
	// 入れた版の install のフックがまだ通っていない。組み立てるときに書き、フックが通ったら外す（フックが失敗して
	// 新しい版を残したときも、フックの途中で止まったときも残る）。前のタグ（previousTag）をこれで決める
	// （<Work>/old/<name>/ の残り方から推し量らない）。
	HookPending bool `json:"hook_pending,omitempty"`
}

// trimBOM は先頭の UTF-8 の BOM を除く（記録とフックの出力で許す。同じ決まりを 1 か所に置く）。
func trimBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
}

// writeRecord は記録を dir に書く。
func writeRecord(dir string, record *Record) error {
	// 文字列・文字列の配列・真偽値だけの構造体なので、失敗しない。
	data, _ := json.MarshalIndent(record, "", "\t")
	return replaceFile(filepath.Join(dir, RecordFile), append(data, '\n'))
}

// clearHookPending は、dir の記録から hook_pending を外す（無ければ何もしない）。読んだ JSON の項目をそのまま
// 書き戻し、この CLI の知らない項目（新しい CLI が書いたもの）も落とさない。
func clearHookPending(dir string) error {
	path := filepath.Join(dir, RecordFile)
	// フックが記録を消した・壊したときも、読めないという 1 つの誤りにする（知らせるだけで、入れたことは変わらない）。
	var fields map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(trimBOM(data), &fields)
	}
	if err != nil {
		return err
	}
	if _, ok := fields["hook_pending"]; !ok {
		return nil
	}
	delete(fields, "hook_pending")
	// 読めた JSON の値だけなので、失敗しない。
	out, _ := json.MarshalIndent(fields, "", "\t")
	return replaceFile(path, append(out, '\n'))
}

// replaceFile は path を data で置き換える。隣に書いてから名前の変更で置き換え、書く途中で止まっても（電源が
// 落ちた）空や半分の記録を残さない（読めない記録では install も uninstall も通らなくなる）。
func replaceFile(path string, data []byte) error {
	// 隣の名前は回ごとに違う名前にする（前からあるファイルを上書きしたり、失敗したときに消したりしない）。
	temp := path + ".new-" + stamp()
	err := writeFile(temp, data, 0o644)
	if err == nil {
		err = rename(temp, path)
	}
	// 書けなかった・置き換えられなかった隣のファイル（このとき書いたもの）は、プラグインのフォルダに残さない。
	if err != nil {
		_ = removeFile(temp)
	}
	return err
}

// readRecord はフォルダの記録を読む。無ければ os.ErrNotExist を包んだ誤り。知らない項目は
// 無視し、知らない版・欠けた項目・正しくないフックは読めないものとする。
func readRecord(dir string) (*Record, error) {
	data, err := os.ReadFile(filepath.Join(dir, RecordFile))
	if err != nil {
		return nil, err
	}
	// 先頭の BOM は許す（利用者が BOM を書くエディタで保存した記録も読む。フックの出力と同じ）。
	data = trimBOM(data)
	var raw struct {
		RecordVersion *int `json:"record_version"`
		Record
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not a record: %v", err)
	}
	switch {
	case raw.RecordVersion == nil:
		return nil, errors.New("record_version is missing")
	case *raw.RecordVersion != recordVersion:
		return nil, fmt.Errorf("unknown record_version %d (this vw2026 reads %d)", *raw.RecordVersion, recordVersion)
	case raw.Name == "" || raw.Registry == "" || raw.Target == "" || raw.Module == "":
		return nil, errors.New("the record lacks name, registry, target or module")
	case !registry.ValidTag(raw.Tag):
		// タグは初回のスクリプトの URL と、利用者に示すコマンドに入る。
		return nil, fmt.Errorf("the record has an invalid tag %q", raw.Tag)
	}
	if err := raw.Hooks.Check(); err != nil {
		return nil, err
	}
	record := raw.Record
	record.RecordVersion = *raw.RecordVersion
	return &record, nil
}

// moduleExists は、記録の module がフォルダの直下に実在するか（uninstall の安全弁）。
func (r *Record) moduleExists(dir string) bool {
	// フォルダの直下の 1 つの名前だけ。. と記録そのものは、いつも在るので歯止めにならない（守ること 9）。
	if r.Module != filepath.Base(r.Module) || !filepath.IsLocal(r.Module) || r.Module == "." || r.Module == RecordFile {
		return false
	}
	return exists(filepath.Join(dir, r.Module))
}

// checkedRecord は dir の記録を読み（読めなければ nil）、記録の名前が name で、記録の module が実在するかを
// 返す。消すコードの歯止め（守ること 9）の確かめは、どこもこれだけを使う。
func checkedRecord(dir, name string) (*Record, bool) {
	record, err := readRecord(dir)
	if err != nil {
		return nil, false
	}
	return record, record.Name == name && record.moduleExists(dir)
}

// validRecord は、dir に名前の一致する記録があり、記録の module が実在するか。
func validRecord(dir, name string) bool {
	_, ok := checkedRecord(dir, name)
	return ok
}

// assemble は zip を取って照らし、<Work>/staging/<name>/ に展開して、モジュールとフックを確かめ、
// 記録を書く。
func (o *Options) assemble(p places, name, source, tag string, asset registry.Asset) (*Record, error) {
	if err := o.download(asset.URL, p.stageZip, asset.SHA256); err != nil {
		return nil, err
	}
	top, err := extract(p.stageZip, p.stageDir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", asset.URL, err)
	}
	module, err := findModule(p.stageDir, top, o.Target)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", asset.URL, err)
	}
	// フックのスクリプトがあることと、インタプリタがこの OS で走ることを、付け替えの前に確かめる（走らない
	// フックに付け替えたあとで気付くと、その間に起動した Vectorworks の下に入れ終えられない版が残る）。
	for _, hook := range [][]string{asset.Hooks.Install, asset.Hooks.Uninstall} {
		if hook == nil {
			continue
		}
		if _, err := scriptIn(p.stageDir, hook); err != nil {
			return nil, fmt.Errorf("%s: %w", asset.URL, err)
		}
		if slices.Contains(registry.Interpreters, hook[0]) {
			if _, err := o.interpreter(hook[0]); err != nil {
				return nil, fmt.Errorf("%s: %w", asset.URL, err)
			}
		}
	}
	// フックが通るまでは hook_pending（付け替えのあとフックが失敗して残した・フックの途中で止まった版を見分ける）。
	record := &Record{
		RecordVersion: recordVersion,
		Name:          name,
		Registry:      source,
		Tag:           tag,
		Target:        o.Target,
		Module:        module,
		Hooks:         asset.Hooks,
		HookPending:   true,
	}
	if err := writeRecord(p.stageDir, record); err != nil {
		return nil, err
	}
	return record, nil
}

// extract は zip を dir へ展開する（docs/registry.md「zip の形」）。展開先の外へ出る項目・
// シンボリックリンク・\ を区切りに使った項目・展開すると maxExtracted を超える zip・項目が maxEntries を超える zip
// は断る。
// 権限（実行ビット）は保ち、ファイルの時刻は展開した時刻のままにする（古い時刻を付けると、
// 付け替えの前に OS の一時フォルダの片付けで消されうるため）。
//
// 直下の項目の名前（モジュールを探すため）を返す。
func extract(zipPath, dir string) ([]string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("not a zip: %w", err)
	}
	defer reader.Close()
	// 項目の数を、展開する前に確かめる。
	if len(reader.File) > maxEntries {
		return nil, fmt.Errorf("the zip has more than %d entries", maxEntries)
	}
	// 展開した大きさの合計を、展開する前に項目の大きさで確かめる（archive/zip は、項目の大きさを超えて
	// 読ませない）。
	var total uint64
	for _, file := range reader.File {
		if file.UncompressedSize64 > uint64(maxExtracted)-total {
			return nil, fmt.Errorf("the zip expands to more than %d bytes", maxExtracted)
		}
		total += file.UncompressedSize64
	}
	if err := mkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var top []string
	for _, file := range reader.File {
		name, err := extractFile(file, dir)
		if err != nil {
			return nil, err
		}
		if name != "." && !seen[name] {
			seen[name] = true
			top = append(top, name)
		}
	}
	return top, nil
}

// extractFile は項目を 1 つ dir へ展開し、その一番上の名前を返す（findModule が dir の中で使うので、
// 名前を確かめたあとの値から作る）。
func extractFile(file *zip.File, dir string) (string, error) {
	name := strings.TrimSuffix(file.Name, "/")
	if err := registry.CheckInside(name); err != nil {
		return "", fmt.Errorf("the entry %q: %w", file.Name, err)
	}
	// ./ で始まる項目（zip -r x.zip ./ が作る）も、整えてから一番上の名前を取る。
	top, _, _ := strings.Cut(path.Clean(name), "/")
	if err := writeEntry(file, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
		return "", err
	}
	return top, nil
}

// writeEntry は、確かめた項目を path に書く（フォルダなら作る）。
func writeEntry(file *zip.File, path string) error {
	mode := file.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return fmt.Errorf("the entry %q is a symbolic link", file.Name)
	case mode.IsDir():
		return mkdirAll(path, 0o755)
	case !mode.IsRegular():
		return fmt.Errorf("the entry %q is not a file", file.Name)
	}
	if err := mkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 持ち主が読み書きできるようにして、ほかの権限（実行ビット）は zip のまま保つ。
	perm := mode.Perm() | 0o600
	in, err := file.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	// O_EXCL で、同じ名前の項目が 2 つあれば断る。
	out, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("the entry %q: %w", file.Name, err)
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// findModule は、dir に展開した直下の項目の名前 top のうちモジュール（mac は *.vwlibrary のフォルダ、
// Windows は *.vlb のファイル）がちょうど 1 つあることを確かめ、その名前を返す。拡張子だけでなく、
// フォルダかファイルかも見る（Vectorworks が読み込めない形の zip を入れない）。
func findModule(dir string, top []string, target string) (string, error) {
	ext, folder, kind := ".vlb", false, "file"
	if strings.HasPrefix(target, "macos-") {
		ext, folder, kind = ".vwlibrary", true, "folder"
	}
	var found []string
	for _, name := range top {
		if strings.EqualFold(filepath.Ext(name), ext) {
			found = append(found, name)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("the zip has no *%s at the top", ext)
	case 1:
		// 展開した直後なので在る。在るかどうかの失敗も、形の違いと同じに断る。
		info, err := os.Lstat(filepath.Join(dir, found[0]))
		if err != nil || info.IsDir() != folder {
			return "", fmt.Errorf("%s at the top of the zip is not a %s (a *%s is a %s)", found[0], kind, ext, kind)
		}
		return found[0], nil
	}
	return "", fmt.Errorf("the zip has %d *%s at the top (%s); it must have exactly one", len(found), ext,
		strings.Join(found, ", "))
}
