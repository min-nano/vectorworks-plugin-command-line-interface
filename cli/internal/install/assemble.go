package install

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// RecordFile は入れたときの記録（docs/registry.md「入れたときの記録」）。
const RecordFile = "vw2026-install.json"

// RecordVersion は読み書きする record_version。
const RecordVersion = 1

// Record は入れたときの記録。プラグインのフォルダの中にだけ置き、フォルダと一緒に付け替える。
type Record struct {
	RecordVersion int            `json:"record_version"`
	Name          string         `json:"name"`
	Registry      string         `json:"registry"`
	Tag           string         `json:"tag"`
	Target        string         `json:"target"`
	Module        string         `json:"module"`
	Hooks         registry.Hooks `json:"hooks"`
}

// readRecord はフォルダの記録を読む。無ければ os.ErrNotExist を包んだ誤り。知らない項目は
// 無視し、知らない版・欠けた項目・正しくないフックは読めないものとする。
func readRecord(dir string) (*Record, error) {
	data, err := os.ReadFile(filepath.Join(dir, RecordFile))
	if err != nil {
		return nil, err
	}
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
	case *raw.RecordVersion != RecordVersion:
		return nil, fmt.Errorf("unknown record_version %d (this vw2026 reads %d)", *raw.RecordVersion, RecordVersion)
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
	if r.Module != filepath.Base(r.Module) || !filepath.IsLocal(r.Module) {
		return false
	}
	return exists(filepath.Join(dir, r.Module))
}

// validRecord は、dir に名前の一致する記録があり、記録の module が実在するか。
func validRecord(dir, name string) bool {
	record, err := readRecord(dir)
	return err == nil && record.Name == name && record.moduleExists(dir)
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
	module, err := findModule(top, o.Target)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", asset.URL, err)
	}
	for _, hook := range [][]string{asset.Hooks.Install, asset.Hooks.Uninstall} {
		if hook == nil {
			continue
		}
		if _, err := scriptIn(p.stageDir, hook); err != nil {
			return nil, fmt.Errorf("%s: %w", asset.URL, err)
		}
	}
	record := &Record{
		RecordVersion: RecordVersion,
		Name:          name,
		Registry:      source,
		Tag:           tag,
		Target:        o.Target,
		Module:        module,
		Hooks:         asset.Hooks,
	}
	// 文字列と文字列の配列だけの構造体なので、失敗しない。
	data, _ := json.MarshalIndent(record, "", "\t")
	if err := writeFile(filepath.Join(p.stageDir, RecordFile), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	return record, nil
}

// extract は zip を dir へ展開する（docs/registry.md「zip の形」）。展開先の外へ出る項目・
// シンボリックリンク・\ を区切りに使った項目は断る。権限（実行ビット）は保ち、ファイルの時刻は
// 展開した時刻のままにする（古い時刻を付けると、付け替えの前に OS の一時フォルダの片付けで消され
// うるため）。
//
// 直下の項目の名前（モジュールを探すため）を返す。
func extract(zipPath, dir string) ([]string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("not a zip: %w", err)
	}
	defer reader.Close()
	if err := mkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var top []string
	for _, file := range reader.File {
		if err := extractFile(file, dir); err != nil {
			return nil, err
		}
		// ./ で始まる項目（zip -r x.zip ./ が作る）も、整えてから一番上の名前を取る。
		name, _, _ := strings.Cut(path.Clean(file.Name), "/")
		if name != "." && !seen[name] {
			seen[name] = true
			top = append(top, name)
		}
	}
	return top, nil
}

func extractFile(file *zip.File, dir string) error {
	name := strings.TrimSuffix(file.Name, "/")
	switch {
	case strings.Contains(file.Name, `\`):
		return fmt.Errorf("the entry %q uses \\ (the separator is /)", file.Name)
	case name == "" || strings.HasPrefix(name, "/") || !filepath.IsLocal(filepath.FromSlash(name)):
		return fmt.Errorf("the entry %q is outside the folder", file.Name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("the entry %q is outside the folder", file.Name)
		}
	}
	mode := file.Mode()
	path := filepath.Join(dir, filepath.FromSlash(name))
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

// findModule は、直下の項目の名前 top のうちモジュール（mac は *.vwlibrary、Windows は *.vlb）が
// ちょうど 1 つあることを確かめ、その名前を返す。
func findModule(top []string, target string) (string, error) {
	ext := ".vlb"
	if strings.HasPrefix(target, "macos-") {
		ext = ".vwlibrary"
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
		return found[0], nil
	}
	return "", fmt.Errorf("the zip has %d *%s at the top (%s); it must have exactly one", len(found), ext,
		strings.Join(found, ", "))
}
