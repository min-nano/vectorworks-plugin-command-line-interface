# PowerShell のファイルを PSScriptAnalyzer で整形・検査し、Pester でテストする（docs/format.md・
# docs/lint.md・docs/plugin/install-and-update.md「テスト」）。scripts/format.sh と scripts/lint.sh と
# hooks.yml が呼ぶ。設定はリポジトリのルートの PSScriptAnalyzerSettings.psd1。
#
#   pwsh scripts/powershell.ps1 -Mode Check <files>  整形が要るファイルを並べて 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Fix <files>    整形して書き換える
#   pwsh scripts/powershell.ps1 -Mode Lint <files>   検査の指摘を並べて、あれば 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Test <files>   Pester でテストし、失敗があれば 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Install        固定した版の PSScriptAnalyzer と Pester を入れる
param(
	[Parameter(Mandatory)]
	[ValidateSet('Check', 'Fix', 'Lint', 'Test', 'Install')]
	[string] $Mode,
	[Parameter(ValueFromRemainingArguments)]
	[string[]] $Path = @()
)

$ErrorActionPreference = 'Stop'

# 版を固定する（手元と CI で結果を揃える）。上げるときはここだけを直す。
$version = '1.25.0'
# Pester は Windows に入っている 3.4 を使わない（フックのテストは Pester 5 で書く）。
$pesterVersion = '5.7.1'

if ($Mode -eq 'Install') {
	Install-Module PSScriptAnalyzer -RequiredVersion $version -Scope CurrentUser -Force
	# Windows に入っている Pester 3.4 と発行元が違うので、発行元の確かめを飛ばして横に入れる。
	Install-Module Pester -RequiredVersion $pesterVersion -Scope CurrentUser -Force -SkipPublisherCheck
	exit 0
}

if ($Mode -eq 'Test') {
	Import-Module Pester -RequiredVersion $pesterVersion
	$configuration = New-PesterConfiguration
	$configuration.Run.Path = $Path
	$configuration.Run.Exit = $true
	$configuration.Output.Verbosity = 'Detailed'
	Invoke-Pester -Configuration $configuration
	exit 0
}

Import-Module PSScriptAnalyzer -RequiredVersion $version
$settings = Join-Path (Split-Path $PSScriptRoot) 'PSScriptAnalyzerSettings.psd1'
# 書き戻すときは BOM を付ける（.editorconfig）。
$utf8 = [System.Text.UTF8Encoding]::new($true)

# Invoke-Analyzer は 1 つのファイルを検査する。Invoke-ScriptAnalyzer は、同じファイルでもときどき解析器の
# 中の NullReferenceException で落ちる（指摘ではなく、走らせ直すと通る。PSScriptAnalyzer #1351）。その失敗
# だけを、どのファイルで何が落ちたかを標準エラーに書いてから、3 回まで走らせ直す。指摘は走らせ直さない。
#
# try / catch は 1 行に書く（整形の規則 PSPlaceCloseBrace が } と catch の間で改行するため。1 行の
# ブロックは規則が見ない）。
function Invoke-Analyzer {
	param([string] $File)
	for ($attempt = 1; ; $attempt++) {
		try { return Invoke-ScriptAnalyzer -Path $File -Settings $settings } catch { Resolve-AnalyzerCrash -File $File -Attempt $attempt -ErrorRecord $_ }
	}
}

# Resolve-AnalyzerCrash は、解析器の NullReferenceException なら理由と場所を書いて戻り（走らせ直す）、
# それ以外か 3 回目なら投げ直す。
function Resolve-AnalyzerCrash {
	param([string] $File, [int] $Attempt, [System.Management.Automation.ErrorRecord] $ErrorRecord)
	if ($Attempt -ge 3 -or $ErrorRecord.Exception.Message -notlike '*Object reference not set*') {
		throw $ErrorRecord
	}
	[Console]::Error.WriteLine(('{0}: PSScriptAnalyzer crashed; running it again: {1}' -f $File, $ErrorRecord.Exception))
}

$status = 0
foreach ($file in $Path) {
	if ($Mode -eq 'Lint') {
		$found = Invoke-Analyzer -File $file
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
	# else を書かない（整形の規則が } と else の間で改行し、else を空白で字下げするため）。
	if ($Mode -eq 'Fix') {
		[System.IO.File]::WriteAllText($file, $formatted, $utf8)
		continue
	}
	$file
	$status = 1
}
exit $status
