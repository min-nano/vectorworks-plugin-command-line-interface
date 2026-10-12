# PowerShell のファイルを PSScriptAnalyzer で整形・検査する（docs/format.md・docs/lint.md）。
# scripts/format.sh と scripts/lint.sh が呼ぶ。設定はリポジトリのルートの PSScriptAnalyzerSettings.psd1。
#
#   pwsh scripts/powershell.ps1 -Mode Check <files>  整形が要るファイルを並べて 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Fix <files>    整形して書き換える
#   pwsh scripts/powershell.ps1 -Mode Lint <files>   検査の指摘を並べて、あれば 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Install        固定した版の PSScriptAnalyzer を入れる
param(
	[Parameter(Mandatory)]
	[ValidateSet('Check', 'Fix', 'Lint', 'Install')]
	[string] $Mode,
	[Parameter(ValueFromRemainingArguments)]
	[string[]] $Path = @()
)

$ErrorActionPreference = 'Stop'

# 版を固定する（手元と CI で結果を揃える）。上げるときはここだけを直す。
$version = '1.25.0'

if ($Mode -eq 'Install') {
	Install-Module PSScriptAnalyzer -RequiredVersion $version -Scope CurrentUser -Force
	exit 0
}

Import-Module PSScriptAnalyzer -RequiredVersion $version
$settings = Join-Path (Split-Path $PSScriptRoot) 'PSScriptAnalyzerSettings.psd1'
# 書き戻すときは BOM を付ける（.editorconfig）。
$utf8 = [System.Text.UTF8Encoding]::new($true)

$status = 0
foreach ($file in $Path) {
	if ($Mode -eq 'Lint') {
		$found = Invoke-ScriptAnalyzer -Path $file -Settings $settings
		if ($found) {
			$found | ForEach-Object {
				'{0}:{1}:{2}: {3}: {4}' -f $file, $_.Line, $_.Column, $_.RuleName, $_.Message
			}
			$status = 1
		}
		continue
	}
	$text = [System.IO.File]::ReadAllText($file)
	$formatted = Invoke-Formatter -ScriptDefinition $text -Settings $settings
	if ($formatted -ceq $text) {
		continue
	}
	if ($Mode -eq 'Fix') {
		[System.IO.File]::WriteAllText($file, $formatted, $utf8)
	}
 else {
		$file
		$status = 1
	}
}
exit $status
