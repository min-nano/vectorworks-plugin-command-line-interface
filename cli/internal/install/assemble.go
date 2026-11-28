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
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// RecordFile はインストールの記録（docs/registry.md「インストールの記録」。2025 は
// vw2025-install.json）。
const RecordFile = edition.Command + "-install.json"

// recordVersion は読み取りと書き込みの対象とする record_version。
const recordVersion = 1

// Record はインストールの記録。プラグインのフォルダの中にのみ配置し、フォルダと共に差し替える。
type Record struct {
	RecordVersion int            `json:"record_version"`
	Name          string         `json:"name"`
	Registry      string         `json:"registry"`
	Tag           string         `json:"tag"`
	Target        string         `json:"target"`
	Module        string         `json:"module"`
	Hooks         registry.Hooks `json:"hooks"`
	// インストールした版の install のフックがまだ通過していない。構築する際に書き込み、
	// フックが通過したら除去する（フックが失敗して新しい版を残置した場合も、フックの途中で停止した
	// 場合も残る）。前のタグ（previousTag）はこれで決定する（<Work>/old/<name>/ の残存の状態から
	// 推測しない）。
	HookPending bool `json:"hook_pending,omitempty"`
}

// trimBOM は先頭の UTF-8 の BOM を除去する（記録とフックの出力で許容する。同一の規則を 1 か所に
// 配置する）。
func trimBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
}

// writeRecord は記録を dir に書き込む。
func writeRecord(dir string, record *Record) error {
	// 文字列・文字列の配列・真偽値のみの構造体であるため、失敗しない。
	data, _ := json.MarshalIndent(record, "", "\t")
	return replaceFile(filepath.Join(dir, RecordFile), append(data, '\n'))
}

// clearHookPending は、dir の記録から hook_pending を除去する（無ければ何もしない）。
// 読み取った JSON の項目をそのまま書き戻し、この CLI の知らない項目（新しい CLI が
// 書き込んだもの）も欠落させない。
func clearHookPending(dir string) error {
	path := filepath.Join(dir, RecordFile)
	// フックが記録を削除した・破損させた場合も、読み取れないという 1 つの誤りとする
	// （通知するのみであり、インストールしたことは変わらない）。
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
	// 読み取れた JSON の値のみであるため、失敗しない。
	out, _ := json.MarshalIndent(fields, "", "\t")
	return replaceFile(path, append(out, '\n'))
}

// replaceFile は path を data で置換する。隣に書き込んでから名前の変更で置換し、
// 書き込みの途中で停止しても（電源が落ちた）空や半分の記録を残置しない（読み取れない記録では
// install も uninstall も通過しなくなる）。
func replaceFile(path string, data []byte) error {
	// 隣の名前は回ごとに異なる名前とする（以前から存在するファイルを上書きしたり、
	// 失敗した場合に削除したりしない）。
	temp := path + ".new-" + stamp()
	err := writeFile(temp, data, 0o644)
	if err == nil {
		err = rename(temp, path)
	}
	// 書き込めなかった・置換できなかった隣のファイル（この回に書き込んだもの）は、
	// プラグインのフォルダに残置しない。
	if err != nil {
		_ = removeFile(temp)
	}
	return err
}

// readRecord はフォルダの記録を読み取る。無ければ os.ErrNotExist を包んだ誤り。
// 知らない項目は無視し、知らない版・欠落した項目・正しくないフックは読み取れないものとする。
func readRecord(dir string) (*Record, error) {
	data, err := os.ReadFile(filepath.Join(dir, RecordFile))
	if err != nil {
		return nil, err
	}
	// 先頭の BOM は許容する（利用者が BOM を書き込むエディタで保存した記録も読み取る。
	// フックの出力と同様）。
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
		return nil, fmt.Errorf("unknown record_version %d (this "+edition.Command+" reads %d)", *raw.RecordVersion, recordVersion)
	case raw.Name == "" || raw.Registry == "" || raw.Target == "" || raw.Module == "":
		return nil, errors.New("the record lacks name, registry, target or module")
	case !registry.ValidTag(raw.Tag):
		// タグは初回スクリプトの URL と、利用者に示すコマンドに含まれる。
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
	// フォルダの直下の 1 つの名前のみ。. と記録そのものは、常に存在するため制約にならない
	// （遵守事項 9）。
	if r.Module != filepath.Base(r.Module) || !filepath.IsLocal(r.Module) || r.Module == "." || r.Module == RecordFile {
		return false
	}
	return exists(filepath.Join(dir, r.Module))
}

// checkedRecord は dir の記録を読み取り（読み取れなければ nil）、記録の名前が name で、
// 記録の module が実在するかを返す。削除処理の制約（遵守事項 9）の検証は、全ての箇所でこれのみを
// 使用する。
func checkedRecord(dir, name string) (*Record, bool) {
	record, err := readRecord(dir)
	if err != nil {
		return nil, false
	}
	return record, record.Name == name && record.moduleExists(dir)
}

// validRecord は、dir に名前の一致する記録が存在し、記録の module が実在するか。
func validRecord(dir, name string) bool {
	_, ok := checkedRecord(dir, name)
	return ok
}

// assemble は zip を取得して照合し、<Work>/staging/<name>/ に展開して、モジュールとフックを検証し、
// 記録を書き込む。
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
	// フックのスクリプトが存在することと、インタプリタがこの OS で実行できることを、
	// 差し替えの前に確認する（実行できないフックに差し替えた後で気付くと、その間に起動した
	// Vectorworks の下にインストールを完了できない版が残る）。
	for _, hook := range [][]string{asset.Hooks.Install, asset.Hooks.Uninstall} {
		if hook == nil {
			continue
		}
		if _, err := scriptIn(p.stageDir, hook); err != nil {
			return nil, fmt.Errorf("%s: %w", asset.URL, err)
		}
		if registry.ViaInterpreter(hook) {
			if _, err := o.interpreter(hook[0]); err != nil {
				return nil, fmt.Errorf("%s: %w", asset.URL, err)
			}
		}
	}
	// フックが通過するまでは hook_pending（差し替えの後フックが失敗して残置した・フックの途中で
	// 停止した版を区別する）。
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

// extract は zip を dir へ展開する（docs/registry.md「zip の構成」）。展開先の外へ出る項目・
// シンボリックリンク・\ を区切りに使用した項目・展開すると maxExtracted を超える zip・項目が
// maxEntries を超える zip は拒否する。
// 権限（実行ビット）は保持し、ファイルの時刻は展開した時刻のままとする（古い時刻を付けると、
// 差し替えの前に OS の一時フォルダの整理で削除されうるため）。
//
// 直下の項目の名前（モジュールを探索するため）を返す。
func extract(zipPath, dir string) ([]string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("not a zip: %w", err)
	}
	defer reader.Close()
	// 項目の数を、展開する前に確認する。
	if len(reader.File) > maxEntries {
		return nil, fmt.Errorf("the zip has more than %d entries", maxEntries)
	}
	// 展開した大きさの合計を、展開する前に項目の大きさで確認する（archive/zip は、
	// 項目の大きさを超えて読み取らせない）。
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

// extractFile は項目を 1 つ dir へ展開し、その最上位の名前を返す（findModule が dir の中で
// 使用するため、名前を検証した後の値から生成する）。
func extractFile(file *zip.File, dir string) (string, error) {
	name := strings.TrimSuffix(file.Name, "/")
	if err := registry.CheckInside(name); err != nil {
		return "", fmt.Errorf("the entry %q: %w", file.Name, err)
	}
	// ./ で始まる項目（zip -r x.zip ./ が生成する）も、正規化してから最上位の名前を取得する。
	top, _, _ := strings.Cut(path.Clean(name), "/")
	if err := writeEntry(file, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
		return "", err
	}
	return top, nil
}

// writeEntry は、検証した項目を path に書き込む（フォルダならば作成する）。
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
	// 所有者が読み書きできるようにし、他の権限（実行ビット）は zip のまま保持する。
	perm := mode.Perm() | 0o600
	in, err := file.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	// O_EXCL で、同じ名前の項目が 2 つ存在すれば拒否する。
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

// findModule は、dir に展開した直下の項目の名前 top の内、モジュール（mac は *.vwlibrary の
// フォルダ、Windows は *.vlb のファイル）が丁度 1 つ存在することを確認し、その名前を返す。
// 拡張子のみでなく、フォルダかファイルかも確認する（Vectorworks が読み込めない形の zip を
// インストールしない）。
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
		// 展開した直後であるため存在する。存在するか否かの確認の失敗も、形の相違と同様に拒否する。
		info, err := os.Lstat(filepath.Join(dir, found[0]))
		if err != nil || info.IsDir() != folder {
			return "", fmt.Errorf("%s at the top of the zip is not a %s (a *%s is a %s)", found[0], kind, ext, kind)
		}
		return found[0], nil
	}
	return "", fmt.Errorf("the zip has %d *%s at the top (%s); it must have exactly one", len(found), ext,
		strings.Join(found, ", "))
}
