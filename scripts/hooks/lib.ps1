# cli の install・uninstall のフック（install.ps1・uninstall.ps1）が読み込む関数
# （docs/plugin/install-and-update.md「`cli` のフック」
# 「Windows の `Path` の読み取りと書き込み」）。
# Path の項目の比較方法・追加方法・除去方法を 1 か所に配置し、install と uninstall が同一の
# 比較方法を使用するようにする。
#
# 利用者の Path の読み書きは「配置場所」（Read・Write・Notify のスクリプトブロックを持つ
# ハッシュテーブル）を経由する。本番環境は Get-RegistryPathStore（HKCU\Environment）で、テストは
# 擬似の配置場所をフックの -Store に渡して差し替える。Windows PowerShell 5.1 で実行する。

# Get-PathEntryKey は、比較のための項目の形式。前後の空白を除去し、全体を囲む " を 1 組のみ除去し、
# / を \ に統一し（Windows は Path の / も区切りとして扱う）、末尾の \ を除去する（ドライブの根の
# C:\ は \ を 1 つ残す。C: は「C ドライブの現在のディレクトリ」になるため）。%…% は展開しない
# （大文字と小文字は Test-PathEntryTarget が区別せずに比較する）。
function Get-PathEntryKey {
	param([string] $Entry)
	$key = $Entry.Trim()
	if ($key.Length -ge 2 -and $key.StartsWith('"') -and $key.EndsWith('"')) {
		$key = $key.Substring(1, $key.Length - 2)
	}
	$key = $key.Replace('/', '\')
	$trimmed = $key.TrimEnd('\')
	if ($trimmed.Length -eq 2 -and $trimmed[1] -eq ':' -and $key.Length -gt 2) {
		return $trimmed + '\'
	}
	return $trimmed
}

# Test-PathEntryAbsolute は、項目の形式 $Key が絶対パスか。ドライブの根からのもの（C:\…）と共有
# （\\server\share\…）のみを絶対とみなす。C: や C:x（ドライブの現在のディレクトリから）・\x（現在の
# ドライブの根から）は、IsPathRooted が真でも絶対ではない（フックの現在のディレクトリやドライブで
# 解決されてしまう）。
function Test-PathEntryAbsolute {
	param([string] $Key)
	return ($Key -match '^[A-Za-z]:\\') -or $Key.StartsWith('\\')
}

# Get-FinalDirectory は、存在するディレクトリ $Dir の実際のパス（GetFinalPathNameByHandle。
# ジャンクション・シンボリックリンク・8.3 形式を解決し、大文字と小文字を統一する）。開けなければ
# $null。
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
	# 読み書きを要求せずに開く（属性のみ）。共有は全て許可し、ディレクトリを開くため
	# FILE_FLAG_BACKUP_SEMANTICS。
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
	# \\?\C:\… は C:\…、\\?\UNC\server\… は \\server\… とする。
	if ($path.StartsWith('\\?\UNC\', [StringComparison]::OrdinalIgnoreCase)) {
		return '\\' + $path.Substring(8)
	}
	if ($path.StartsWith('\\?\', [StringComparison]::Ordinal)) {
		return $path.Substring(4)
	}
	return $path
}

# Get-PathEntryPlace は、比較のための項目 $Key（Get-PathEntryKey の形式）の実際の場所。存在する最も
# 近いディレクトリを Get-FinalDirectory で解決し、その下の存在しない部分は名前のまま付加する
# （uninstall のフックはフォルダを移動した後で実行され、<Plug-Ins>\cli\ は既に存在しないため、
# 存在する親まで遡る）。%…% を含む項目・絶対でない項目・解決できないものは $null
# （同じとみなさない）。
function Get-PathEntryPlace {
	param([string] $Key)
	if ($Key.Contains('%')) {
		return $null
	}
	# 記述の誤り（使用できない文字など）や、解決のための呼び出しの失敗は、同じとみなさない。
	try {
		if (-not (Test-PathEntryAbsolute $Key)) {
			return $null
		}
		$dir = [IO.Path]::GetFullPath($Key)
		# 末尾の \ を除去する（ルートの C:\ は除去しない。C: は「C ドライブの現在のディレクトリ」
		# になるため）。
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

# Get-PathEntryTarget は、比較する側の $Dir の形式（Get-PathEntryKey）・根（Get-PathEntryRoot）・
# 根がこの計算機の固定ドライブか（Test-LocalFixedDrive）・実際の場所（Get-PathEntryPlace）。Path の
# 項目ごとに $Dir のディレクトリを開き直さないよう、Path を 1 回走査する間、これを 1 度のみ算出して
# 使用する。
function Get-PathEntryTarget {
	param([string] $Dir)
	$key = Get-PathEntryKey $Dir
	$root = Get-PathEntryRoot $key
	return [pscustomobject]@{
		Key   = $key
		Root  = $root
		Local = (Test-LocalFixedDrive $root)
		Place = (Get-PathEntryPlace $key)
	}
}

# Test-LocalFixedDrive は、根 $Root（Get-PathEntryRoot の形式）が、この計算機の固定ドライブ
# （C: など）か。ネットワークドライブ・取り外し可能なドライブ・共有（\\server\share）・
# 判別できないものは偽。
function Test-LocalFixedDrive {
	param([string] $Root)
	if ($null -eq $Root -or $Root.Length -ne 2 -or $Root[1] -ne ':') {
		return $false
	}
	try {
		return [IO.DriveInfo]::new($Root).DriveType -eq [IO.DriveType]::Fixed
	}
	catch {
		return $false
	}
}

# Get-PathEntryRoot は、比較のための項目 $Key の根（C: や \\server\share。文字列のみで算出し、
# ディスクに触れない）。判別できなければ $null。
function Get-PathEntryRoot {
	param([string] $Key)
	try {
		if ($Key.Contains('%') -or -not (Test-PathEntryAbsolute $Key)) {
			return $null
		}
		return [IO.Path]::GetPathRoot([IO.Path]::GetFullPath($Key)).TrimEnd('\')
	}
	catch {
		return $null
	}
}

# Test-PathEntryTarget は、Path の項目 $Entry が $Target（Get-PathEntryTarget）と同じ項目か。まず
# Get-PathEntryKey の形式を大文字と小文字を区別せずに比較し、異なれば実際の場所
# （Get-PathEntryPlace）で比較する（同じディレクトリを別の表記、例えばジャンクション・8.3 形式・
# --plugins-dir の表記で指したものを同じとみなす。CLI の within と同じ考え方）。
function Test-PathEntryTarget {
	param([string] $Entry, [object] $Target)
	$key = Get-PathEntryKey $Entry
	if ($key.Length -eq 0) {
		return $false
	}
	if ([string]::Equals($key, $Target.Key, [StringComparison]::OrdinalIgnoreCase)) {
		return $true
	}
	if ($null -eq $Target.Place) {
		return $false
	}
	# <Plug-Ins>\cli\bin がこの計算機の固定ドライブにある場合は、根の異なる項目を開かずに
	# 異なるとみなす（到達できない共有の項目ごとに SMB の待機で停止しないため。別の根から
	# 同じ固定ドライブのディレクトリを指す表記、例えば subst のドライブや \\localhost\c$ は
	# 同じとみなさない）。共有・ネットワークドライブにある場合（AppData をリダイレクトした場合など）
	# は、別の根（共有を割り当てたドライブなど）から到達できる項目があるため、全て開いて比較する。
	$root = Get-PathEntryRoot $key
	if ($null -eq $root) {
		return $false
	}
	if ($Target.Local -and -not [string]::Equals($root, $Target.Root, [StringComparison]::OrdinalIgnoreCase)) {
		return $false
	}
	$place = Get-PathEntryPlace $key
	return $null -ne $place -and [string]::Equals($place, $Target.Place, [StringComparison]::OrdinalIgnoreCase)
}

# Test-PathValue は、Path の値 $Value に $Dir と同じ項目が存在するか。
function Test-PathValue {
	param([string] $Value, [string] $Dir)
	$target = Get-PathEntryTarget $Dir
	foreach ($entry in $Value.Split(';')) {
		if (Test-PathEntryTarget $entry $target) {
			return $true
		}
	}
	return $false
}

# Get-PathWithEntry は、$Value の末尾に $Dir を追加した値。値が空か末尾が ; ならば区切りを重ねない
# （空の項目を増やさない）。他の項目の順序・空の項目・前後の空白は変更しない。
function Get-PathWithEntry {
	param([string] $Value, [string] $Dir)
	if ($Value.Length -eq 0 -or $Value.EndsWith(';')) {
		return $Value + $Dir
	}
	return $Value + ';' + $Dir
}

# Get-PathWithoutEntry は、$Value から $Dir と同じ項目を全て除去した値。項目とその前の区切りの ; を
# 除去する（先頭の項目ならば後ろの ;）。; で分割して除去し、残りを ; で連結すると、この除去方法と
# 同じになる。
function Get-PathWithoutEntry {
	param([string] $Value, [string] $Dir)
	$target = Get-PathEntryTarget $Dir
	$kept = @($Value.Split(';') | Where-Object { -not (Test-PathEntryTarget $_ $target) })
	return $kept -join ';'
}

# ConvertTo-HookJson は文字列を JSON の文字列にする。" と \ と制御文字に加えて ASCII の外の文字も
# \uXXXX にし、出力を ASCII のみにする（標準出力の文字コードが何であっても UTF-8 として
# 読み取れる）。
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

# Write-HookOutput は JSON のテキストを標準出力へ 1 度のみ書き込む（パイプラインに値を残さない）。
function Write-HookOutput {
	param([string] $Json)
	[Console]::Out.Write($Json + "`n")
	[Console]::Out.Flush()
}

# Write-HookError は理由を標準エラーへ書き込む。
function Write-HookError {
	param([string] $Message)
	[Console]::Error.WriteLine('cli hook: ' + $Message)
}

# Write-HookException は、何を実行していて失敗したか（$What）と例外の理由を標準エラーへ書き込む。
#
# このファイルの try / catch は 1 行に記述する（整形の規則 PSPlaceCloseBrace の NewLineAfter が } と
# catch の間で改行するため。1 行のブロックは規則が検査しない。scripts/powershell.ps1 が else を
# 記述しないのと同様）。
function Write-HookException {
	param([string] $What, [System.Management.Automation.ErrorRecord] $ErrorRecord)
	Write-HookError ($What + ': ' + $ErrorRecord.Exception.Message)
}

# Initialize-HookConsole は標準出力を UTF-8（BOM なし）にする（Windows PowerShell 5.1 は、
# リダイレクトされた標準出力を OEM のコードページで書き込む）。コンソールの無いプロセスでは
# 設定できずに例外となることがあるが、出力は ASCII のみであるため（ConvertTo-HookJson）、そのまま
# 続行する。
function Initialize-HookConsole {
	$utf8 = [Text.UTF8Encoding]::new($false)
	try { [Console]::OutputEncoding = $utf8 } catch { Write-HookException 'cannot set the encoding' $_ }
}

# Get-HookYear は、渡されたフックの変数の名前から版（'2026' か '2025'）を決定して返す。CLI は自身の
# 版の名前（2025 の CLI は VW2025_HOOK_*）のみを渡すため、2025 と 2026 の zip に同一のフックを
# 格納できる（docs/plugin/editions.md）。いずれも無い場合・両方ある場合は $null（呼び出し元は何も
# 変更せずに失敗で終了する）。
function Get-HookYear {
	$has2025 = $null -ne [Environment]::GetEnvironmentVariable('VW2025_HOOK_INSTALL_DIR')
	$has2026 = $null -ne [Environment]::GetEnvironmentVariable('VW2026_HOOK_INSTALL_DIR')
	if ($has2025 -and $has2026) {
		Write-HookError 'both VW2025_HOOK_INSTALL_DIR and VW2026_HOOK_INSTALL_DIR are set'
		return $null
	}
	if ($has2025) {
		return '2025'
	}
	if ($has2026) {
		return '2026'
	}
	Write-HookError 'VW2026_HOOK_INSTALL_DIR (or VW2025_HOOK_INSTALL_DIR) is not set'
	return $null
}

# Get-HookInstallDir は版 $Year の VW<版>_HOOK_INSTALL_DIR を検証し、末尾の区切りを除去して返す。
# 絶対パスでなければ $null（呼び出し元は何も変更せずに失敗で終了する）。
function Get-HookInstallDir([string] $Year) {
	$name = 'VW' + $Year + '_HOOK_INSTALL_DIR'
	$dir = [Environment]::GetEnvironmentVariable($name)
	if ([string]::IsNullOrEmpty($dir)) {
		Write-HookError ($name + ' is not set')
		return $null
	}
	# IsPathRooted ではなく、Path の項目と同じ検証で判定する（\x や C:x は、現在のドライブや
	# ディレクトリで解決されてしまう）。
	if (-not (Test-PathEntryAbsolute $dir)) {
		Write-HookError ($name + ' is not an absolute path: ' + $dir)
		return $null
	}
	return $dir.TrimEnd('\', '/')
}

# Get-HookTarget は、フックの冒頭で決定するもの（版 Year と <Plug-Ins>\cli\bin の Bin）を返す。
# 決定できなければ、理由を標準エラーへ書き込んで $null を返す（呼び出し元は何も変更せずに失敗で
# 終了する）。exit は呼び出さない（テストがこのファイルをドットソースで読み込むため）。
function Get-HookTarget {
	$year = Get-HookYear
	if ($null -eq $year) {
		return $null
	}
	$installDir = Get-HookInstallDir $year
	if ($null -eq $installDir) {
		return $null
	}
	return @{ Year = $year; Bin = $installDir + '\bin' }
}

# Get-RegistryPathStore は利用者の Path（HKCU\Environment の値 Path）の配置場所。展開しない値を
# 読み取り、型を保持して書き込む（[Environment]::SetEnvironmentVariable は展開した値を REG_SZ で
# 書き戻すとされ、setx は 1024 文字で切り詰めるため使用しない）。開いたキーは例外の場合に閉じない
# （フックは直ちに終了する）。
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
		# 書き込んだことをトップレベルのウィンドウに通知する（WM_SETTINGCHANGE。
		# Explorer などがそこから開く端末に新しい値を渡す）。上限はウィンドウ 1 つずつに順に
		# 適用され、全体の上限は持たない。
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

# Invoke-PathNotify は通知を送信する。失敗は無視する（値は書き込めており、ログインし直せば
# 反映される）。
function Invoke-PathNotify {
	param([hashtable] $Store)
	try { & $Store.Notify } catch { Write-HookException 'cannot broadcast the change of Path' $_ }
}

# Install-PathEntry は利用者の Path に $Dir を追加し、結果（added・exists・conflict・failed）を
# 返す。
#
#   added     末尾に追加した（値が無ければ REG_EXPAND_SZ で作成した）
#   exists    同じ項目が既に存在した（何もしない）
#   conflict  値の型が REG_SZ・REG_EXPAND_SZ のいずれでもない（書き換えない）
#   failed    読み取れない・書き込めなかった
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

# Uninstall-PathEntry は利用者の Path から $Dir と同じ項目を全て除去し、結果（removed・none・
# failed）を返す。除去して空になっても値は削除せず、空の文字列を元の型で書き込む（削除してよいのは
# 一致する項目のみ。遵守事項 9）。失敗は例外とせず、結果で返す（uninstall のフックは失敗しない）。
#
#   removed  項目を除去した
#   none     同じ項目が無かった（値が無い・型が文字列でない場合も）
#   failed   読み取れない・書き込めなかった
function Uninstall-PathEntry {
	param([hashtable] $Store, [string] $Dir)
	try { $current = & $Store.Read } catch { Write-HookException 'cannot read Path' $_; return 'failed' }
	$kind = [string] $current.Kind
	if (-not $current.Exists -or ($kind -ne 'String' -and $kind -ne 'ExpandString')) {
		return 'none'
	}
	$value = [string] $current.Value
	# 1 回のみ走査する（項目ごとの場所の算出は遅い共有で時間を要する）。除去しても同じならば、
	# 一致する項目は無い（; で分割して連結し直すと元の値に戻る）。
	$rest = Get-PathWithoutEntry $value $Dir
	if ($rest -ceq $value) {
		return 'none'
	}
	try { & $Store.Write $rest $kind } catch { Write-HookException 'cannot write Path' $_; return 'failed' }
	Invoke-PathNotify $Store
	return 'removed'
}
