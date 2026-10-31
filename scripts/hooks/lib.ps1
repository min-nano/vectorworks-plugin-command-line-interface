# cli の install・uninstall のフック（install.ps1・uninstall.ps1）が読み込む関数
# （docs/plugin/install-and-update.md「`cli` のフック」「Windows の `Path` の読み書き」）。
# Path の項目の比べ方・足し方・除き方を 1 か所に置き、install と uninstall が同じ比べ方を使うようにする。
#
# 利用者の Path の読み書きは「置き場所」（Read・Write・Delete・Notify のスクリプトブロックを持つ
# ハッシュテーブル）を通す。本番は Get-RegistryPathStore（HKCU\Environment）で、テストは偽の置き場所を
# フックの -Store に渡して差し替える。Windows PowerShell 5.1 で動かす。

# Get-PathEntryKey は、比べるための項目の形。前後の空白を除き、全体を囲む " を 1 組だけ除き、末尾の
# \ を除く。%…% は展開しない（大文字と小文字は Test-PathEntry が区別せずに比べる）。
function Get-PathEntryKey {
	param([string] $Entry)
	$key = $Entry.Trim()
	if ($key.Length -ge 2 -and $key.StartsWith('"') -and $key.EndsWith('"')) {
		$key = $key.Substring(1, $key.Length - 2)
	}
	return $key.TrimEnd('\')
}

# Get-FinalDirectory は、在るディレクトリ $Dir の実際のパス（GetFinalPathNameByHandle。ジャンクション・
# シンボリックリンク・8.3 形式を解き、大文字と小文字を揃える）。開けなければ $null。
function Get-FinalDirectory {
	param([string] $Dir)
	if (-not ('Vw2026Hook.FileMethods' -as [type])) {
		Add-Type -Namespace Vw2026Hook -Name FileMethods -MemberDefinition @'
[DllImport("kernel32.dll", EntryPoint = "CreateFileW", CharSet = CharSet.Unicode, SetLastError = true)]
public static extern IntPtr CreateFile(string name, uint access, uint share, IntPtr security, uint disposition, uint flags, IntPtr template);
[DllImport("kernel32.dll", EntryPoint = "GetFinalPathNameByHandleW", CharSet = CharSet.Unicode, SetLastError = true)]
public static extern uint GetFinalPathNameByHandle(IntPtr handle, System.Text.StringBuilder path, uint size, uint flags);
[DllImport("kernel32.dll", SetLastError = true)]
public static extern bool CloseHandle(IntPtr handle);
'@ | Out-Null
	}
	$native = [Vw2026Hook.FileMethods]
	# 読み書きを求めずに開く（属性だけ）。共有はすべて許し、ディレクトリを開くので FILE_FLAG_BACKUP_SEMANTICS。
	$handle = $native::CreateFile($Dir, 0, 7, [IntPtr]::Zero, 3, 0x02000000, [IntPtr]::Zero)
	if ($handle -eq [IntPtr] -1) {
		return $null
	}
	try {
		$builder = [Text.StringBuilder]::new(32768)
		$length = $native::GetFinalPathNameByHandle($handle, $builder, $builder.Capacity, 0)
		if ($length -eq 0 -or $length -ge $builder.Capacity) {
			return $null
		}
		$path = $builder.ToString()
	}
	finally {
		[void] $native::CloseHandle($handle)
	}
	# \\?\C:\… は C:\…、\\?\UNC\server\… は \\server\… にする。
	if ($path.StartsWith('\\?\UNC\', [StringComparison]::OrdinalIgnoreCase)) {
		return '\\' + $path.Substring(8)
	}
	if ($path.StartsWith('\\?\', [StringComparison]::Ordinal)) {
		return $path.Substring(4)
	}
	return $path
}

# Get-PathEntryPlace は、比べるための項目 $Key（Get-PathEntryKey の形）の実際の場所。在るいちばん近い
# ディレクトリを Get-FinalDirectory で解き、その下の在らない部分は名前のまま付ける（uninstall のフックは
# フォルダを移したあとで走り、<Plug-Ins>\cli\ はもう無いので、在る親までさかのぼる）。%…% を含む項目・
# 絶対でない項目・解けないものは $null（同じとみなさない）。
function Get-PathEntryPlace {
	param([string] $Key)
	if ($Key.Contains('%')) {
		return $null
	}
	# 書き方の誤り（使えない文字など）や、解くための呼び出しの失敗は、同じとみなさない。
	try {
		if (-not [IO.Path]::IsPathRooted($Key)) {
			return $null
		}
		$dir = [IO.Path]::GetFullPath($Key)
		# 末尾の \ を除く（ルートの C:\ は除かない。C: は「C ドライブの今のディレクトリ」になるため）。
		if ($dir.Length -gt [IO.Path]::GetPathRoot($dir).Length) {
			$dir = $dir.TrimEnd('\')
		}
		$rest = ''
		while ($dir -and -not [IO.Directory]::Exists($dir)) {
			$rest = '\' + [IO.Path]::GetFileName($dir) + $rest
			$dir = [IO.Path]::GetDirectoryName($dir)
		}
		if (-not $dir) {
			return $null
		}
		$final = Get-FinalDirectory $dir
	}
	catch {
		return $null
	}
	if ($null -eq $final) {
		return $null
	}
	return $final.TrimEnd('\') + $rest
}

# Test-PathEntry は、Path の項目 $Entry が $Dir と同じ項目か。まず Get-PathEntryKey の形を大文字と小文字を
# 区別せずに比べ、違えば実際の場所（Get-PathEntryPlace）で比べる（同じディレクトリを別の書き方、たとえば
# ジャンクション・8.3 形式・--plugins-dir の書き方で指したものを同じとみなす。CLI の within と同じ考え方）。
function Test-PathEntry {
	param([string] $Entry, [string] $Dir)
	$a = Get-PathEntryKey $Entry
	$b = Get-PathEntryKey $Dir
	if ($a.Length -eq 0) {
		return $false
	}
	if ([string]::Equals($a, $b, [StringComparison]::OrdinalIgnoreCase)) {
		return $true
	}
	$placeA = Get-PathEntryPlace $a
	$placeB = Get-PathEntryPlace $b
	return $null -ne $placeA -and $null -ne $placeB -and [string]::Equals($placeA, $placeB, [StringComparison]::OrdinalIgnoreCase)
}

# Test-PathValue は、Path の値 $Value に $Dir と同じ項目があるか。
function Test-PathValue {
	param([string] $Value, [string] $Dir)
	foreach ($entry in $Value.Split(';')) {
		if (Test-PathEntry $entry $Dir) {
			return $true
		}
	}
	return $false
}

# Get-PathWithEntry は、$Value の末尾に $Dir を足した値。値が空か末尾が ; なら区切りを重ねない
# （空の項目を増やさない）。ほかの項目の並び・空の項目・前後の空白は変えない。
function Get-PathWithEntry {
	param([string] $Value, [string] $Dir)
	if ($Value.Length -eq 0 -or $Value.EndsWith(';')) {
		return $Value + $Dir
	}
	return $Value + ';' + $Dir
}

# Get-PathWithoutEntry は、$Value から $Dir と同じ項目をすべて除いた値。項目とその前の区切りの ; を
# 除く（先頭の項目なら後ろの ;）。; で分けて除き、残りを ; でつなぐと、この除き方と同じになる。
function Get-PathWithoutEntry {
	param([string] $Value, [string] $Dir)
	$kept = @($Value.Split(';') | Where-Object { -not (Test-PathEntry $_ $Dir) })
	return $kept -join ';'
}

# ConvertTo-HookJson は文字列を JSON の文字列にする。" と \ と制御文字に加えて ASCII の外の文字も
# \uXXXX にし、出力を ASCII だけにする（標準出力の文字コードが何であっても UTF-8 として読める）。
function ConvertTo-HookJson {
	param([string] $Text)
	$builder = [Text.StringBuilder]::new('"')
	foreach ($c in $Text.ToCharArray()) {
		if ($c -eq '"' -or $c -eq '\') {
			[void] $builder.Append('\').Append($c)
			continue
		}
		if ([int] $c -lt 0x20 -or [int] $c -gt 0x7e) {
			[void] $builder.AppendFormat('\u{0:x4}', [int] $c)
			continue
		}
		[void] $builder.Append($c)
	}
	return $builder.Append('"').ToString()
}

# Write-HookOutput は JSON のテキストを標準出力へ 1 度だけ書く（パイプラインに値を残さない）。
function Write-HookOutput {
	param([string] $Json)
	[Console]::Out.Write($Json + "`n")
	[Console]::Out.Flush()
}

# Write-HookError は理由を標準エラーへ書く。
function Write-HookError {
	param([string] $Message)
	[Console]::Error.WriteLine('cli hook: ' + $Message)
}

# Write-HookException は、何をしていて失敗したか（$What）と例外の理由を標準エラーへ書く。
#
# このファイルの try / catch は 1 行に書く（整形の規則 PSPlaceCloseBrace の NewLineAfter が } と catch の
# 間で改行するため。1 行のブロックは規則が見ない。scripts/powershell.ps1 が else を書かないのと同じ）。
function Write-HookException {
	param([string] $What, [System.Management.Automation.ErrorRecord] $ErrorRecord)
	Write-HookError ($What + ': ' + $ErrorRecord.Exception.Message)
}

# Initialize-HookConsole は標準出力を UTF-8（BOM なし）にする（Windows PowerShell 5.1 は、リダイレクト
# された標準出力を OEM のコードページで書く）。コンソールの無いプロセスでは設定できずに例外になる
# ことがあるが、出力は ASCII だけなので（ConvertTo-HookJson）、そのまま進める。
function Initialize-HookConsole {
	$utf8 = [Text.UTF8Encoding]::new($false)
	try { [Console]::OutputEncoding = $utf8 } catch { Write-HookException 'cannot set the encoding' $_ }
}

# Get-HookInstallDir は VW2026_HOOK_INSTALL_DIR を確かめて、末尾の区切りを除いて返す。絶対パスで
# なければ $null（呼ぶ側は何も変えずに失敗で終わる）。
function Get-HookInstallDir {
	$dir = $env:VW2026_HOOK_INSTALL_DIR
	if ([string]::IsNullOrEmpty($dir)) {
		Write-HookError 'VW2026_HOOK_INSTALL_DIR is not set'
		return $null
	}
	if (-not [IO.Path]::IsPathRooted($dir)) {
		Write-HookError ('VW2026_HOOK_INSTALL_DIR is not an absolute path: ' + $dir)
		return $null
	}
	return $dir.TrimEnd('\', '/')
}

# Get-RegistryPathStore は利用者の Path（HKCU\Environment の値 Path）の置き場所。展開しない値を読み、
# 型を保って書く（[Environment]::SetEnvironmentVariable は展開した値を REG_SZ で書き戻すとされ、
# setx は 1024 文字で切り詰めるので使わない）。開いたキーは例外のときに閉じない（フックはすぐ終わる）。
function Get-RegistryPathStore {
	return @{
		Read   = {
			$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $false)
			if ($null -eq $key) {
				return @{ Exists = $false; Kind = ''; Value = '' }
			}
			$exists = $key.GetValueNames() -contains 'Path'
			$kind = ''
			$value = ''
			if ($exists) {
				$kind = [string] $key.GetValueKind('Path')
			}
			if ($kind -eq 'String' -or $kind -eq 'ExpandString') {
				$options = [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames
				$value = [string] $key.GetValue('Path', '', $options)
			}
			$key.Close()
			return @{ Exists = $exists; Kind = $kind; Value = $value }
		}
		Write  = {
			param([string] $Value, [string] $Kind)
			$key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
			$key.SetValue('Path', $Value, [Microsoft.Win32.RegistryValueKind] $Kind)
			$key.Close()
		}
		Delete = {
			$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
			if ($null -eq $key) {
				return
			}
			$key.DeleteValue('Path', $false)
			$key.Close()
		}
		# 書いたことをトップレベルのウィンドウに知らせる（WM_SETTINGCHANGE。Explorer などがそこから
		# 開く端末に新しい値を渡す）。上限はウィンドウ 1 つずつに順にかかり、全体の上限は持たない。
		Notify = {
			if (-not ('Vw2026Hook.NativeMethods' -as [type])) {
				Add-Type -Namespace Vw2026Hook -Name NativeMethods -MemberDefinition @'
[DllImport("user32.dll", EntryPoint = "SendMessageTimeoutW", CharSet = CharSet.Unicode, SetLastError = true)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
'@ | Out-Null
			}
			$result = [UIntPtr]::Zero
			# HWND_BROADCAST・WM_SETTINGCHANGE・SMTO_ABORTIFHUNG・1000 ms
			$all = [IntPtr] 0xffff
			$native = [Vw2026Hook.NativeMethods]
			[void] $native::SendMessageTimeout($all, 0x1a, [UIntPtr]::Zero, 'Environment', 0x2, 1000, [ref] $result)
		}
	}
}

# Invoke-PathNotify は知らせを送る。失敗は無視する（値は書けていて、ログインし直せば効く）。
function Invoke-PathNotify {
	param([hashtable] $Store)
	try { & $Store.Notify } catch { Write-HookException 'cannot broadcast the change of Path' $_ }
}

# Install-PathEntry は利用者の Path に $Dir を足し、結果（added・exists・conflict・failed）を返す。
#
#   added     末尾に足した（値が無ければ REG_EXPAND_SZ で作った）
#   exists    同じ項目が既にあった（何もしない）
#   conflict  値の型が REG_SZ・REG_EXPAND_SZ のどちらでもない（書き換えない）
#   failed    読めない・書けなかった
function Install-PathEntry {
	param([hashtable] $Store, [string] $Dir)
	try { $current = & $Store.Read } catch { Write-HookException 'cannot read Path' $_; return 'failed' }
	$kind = 'ExpandString'
	$value = ''
	if ($current.Exists) {
		$kind = [string] $current.Kind
		if ($kind -ne 'String' -and $kind -ne 'ExpandString') {
			Write-HookError ('Path is of the type ' + $kind + '; left it as it is')
			return 'conflict'
		}
		$value = [string] $current.Value
		if (Test-PathValue $value $Dir) {
			return 'exists'
		}
	}
	$next = Get-PathWithEntry $value $Dir
	try { & $Store.Write $next $kind } catch { Write-HookException 'cannot write Path' $_; return 'failed' }
	Invoke-PathNotify $Store
	return 'added'
}

# Uninstall-PathEntry は利用者の Path から $Dir と同じ項目をすべて除き、結果（removed・none・failed）を
# 返す。除いて空になったら値ごと消す。失敗は例外にせず、結果で返す（uninstall のフックは失敗しない）。
#
#   removed  項目を除いた
#   none     同じ項目が無かった（値が無い・型が文字列でないときも）
#   failed   読めない・書けなかった
function Uninstall-PathEntry {
	param([hashtable] $Store, [string] $Dir)
	try { $current = & $Store.Read } catch { Write-HookException 'cannot read Path' $_; return 'failed' }
	$kind = [string] $current.Kind
	if (-not $current.Exists -or ($kind -ne 'String' -and $kind -ne 'ExpandString')) {
		return 'none'
	}
	$value = [string] $current.Value
	if (-not (Test-PathValue $value $Dir)) {
		return 'none'
	}
	$rest = Get-PathWithoutEntry $value $Dir
	$change = { & $Store.Write $rest $kind }
	if ($rest.Length -eq 0) {
		$change = $Store.Delete
	}
	try { & $change } catch { Write-HookException 'cannot write Path' $_; return 'failed' }
	Invoke-PathNotify $Store
	return 'removed'
}
