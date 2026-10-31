# PowerShell のファイルを PSScriptAnalyzer で整形・検査し、Pester でテストする（docs/format.md・
# docs/lint.md・docs/plugin/install-and-update.md「テスト」）。scripts/format.sh と scripts/lint.sh と
# hooks.yml が呼ぶ。設定はリポジトリのルートの PSScriptAnalyzerSettings.psd1。
#
#   pwsh scripts/powershell.ps1 -Mode Check <files>  整形が要るファイルを並べて 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Fix <files>    整形して書き換える
#   pwsh scripts/powershell.ps1 -Mode Lint <files>   検査の指摘を並べて、あれば 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Test <files>   Pester でテストし、失敗があれば 1 で終わる
#   pwsh scripts/powershell.ps1 -Mode Install        固定した版の PSScriptAnalyzer と Pester を入れる
#
# -Mode LintFile <file> は -Mode Lint が子のプロセスで使う（指摘があれば 1、解析器が落ちたら 3 で終わる）。
param(
	[Parameter(Mandatory)]
	[ValidateSet('Check', 'Fix', 'Lint', 'LintFile', 'Test', 'Install')]
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

# Lint は、ファイルごとに子の pwsh（-Mode LintFile）で検査する。PSScriptAnalyzer は、同じファイルでもときどき
# 解析器の中で落ちる（NullReferenceException・PowerShell の実行時の「An item with the same key has already been
# added」でプロセスごと終わる。指摘ではなく、走らせ直すと通る。PSScriptAnalyzer #1351・#1511）。プロセスごと
# 落ちるとスクリプトの中では受け止められないので、子が指摘（1）でも通る（0）でもなく終わったときだけ、そう書いて
# から 3 回まで走らせ直す。指摘は走らせ直さない。
if ($Mode -eq 'Lint') {
	$pwsh = (Get-Process -Id $PID).Path
	$status = 0
	foreach ($file in $Path) {
		for ($attempt = 1; ; $attempt++) {
			& $pwsh -NoProfile -File $PSCommandPath -Mode LintFile $file
			$code = $LASTEXITCODE
			if ($code -le 1 -or $attempt -ge 3) {
				break
			}
			[Console]::Error.WriteLine(('{0}: PSScriptAnalyzer crashed (exit code {1}); running it again' -f $file, $code))
		}
		if ($code -ne 0) {
			$status = 1
		}
	}
	exit $status
}

Import-Module PSScriptAnalyzer -RequiredVersion $version
$settings = Join-Path (Split-Path $PSScriptRoot) 'PSScriptAnalyzerSettings.psd1'
# 書き戻すときは BOM を付ける（.editorconfig）。
$utf8 = [System.Text.UTF8Encoding]::new($true)

# 子のプロセス（LintFile）では、解析器の中の失敗（止まる誤り）を 3 で終わらせ、指摘（1）と分ける。ほかの
# モードでは、これまでどおり止まる（break は誤りをそのまま投げる）。
trap {
	if ($Mode -ne 'LintFile') {
		break
	}
	[Console]::Error.WriteLine(('{0}: {1}' -f ($Path -join ' '), $_.Exception))
	exit 3
}

$status = 0
foreach ($file in $Path) {
	if ($Mode -eq 'LintFile') {
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
	# else を書かない（整形の規則が } と else の間で改行し、else を空白で字下げするため）。
	if ($Mode -eq 'Fix') {
		[System.IO.File]::WriteAllText($file, $formatted, $utf8)
		continue
	}
	$file
	$status = 1
}
exit $status
