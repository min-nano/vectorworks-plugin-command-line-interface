# PowerShell のファイルを PSScriptAnalyzer で整形・検査し、Pester でテストする（docs/format.md・
# docs/lint.md・docs/plugin/install-and-update.md「テスト」）。scripts/format.sh と
# scripts/lint.sh と hooks.yml が呼び出す。設定はリポジトリのルートの
# PSScriptAnalyzerSettings.psd1。
#
#   pwsh scripts/powershell.ps1 -Mode Check <files>  整形が必要なファイルを列挙して 1 で終了する
#   pwsh scripts/powershell.ps1 -Mode Fix <files>    整形して書き換える
#   pwsh scripts/powershell.ps1 -Mode Lint <files>   検査の指摘を列挙し、あれば 1 で終了する
#   pwsh scripts/powershell.ps1 -Mode Test <files>   Pester でテストし、失敗があれば 1 で終了する
#   pwsh scripts/powershell.ps1 -Mode Install        固定版の PSScriptAnalyzer と Pester を導入する
#   pwsh scripts/powershell.ps1 -Mode Install -Module Pester   渡したモジュールのみを導入する
#
# -Mode LintFile <file> は -Mode Lint が子プロセスで使用する（指摘があれば 1、解析器が異常終了した
# 場合は 3 で終了する）。
param(
	[Parameter(Mandatory, Position = 0)]
	[ValidateSet('Check', 'Fix', 'Lint', 'LintFile', 'Test', 'Install')]
	[string] $Mode,
	[Parameter(Position = 1, ValueFromRemainingArguments)]
	[string[]] $Path = @(),
	# -Mode Install でインストールするモジュール。整形と検査は PSScriptAnalyzer のみ、フックの
	# テストは Pester のみを使用する。$Mode と $Path に Position を記述し、これを名前でのみ
	# 渡せるようにする（Position を記述しないと宣言の順に位置が割り当てられ、ファイルの名前を
	# 受け取ってしまう）。
	[ValidateSet('PSScriptAnalyzer', 'Pester')]
	[string[]] $Module = @('PSScriptAnalyzer', 'Pester')
)

$ErrorActionPreference = 'Stop'

# 版を固定する（ローカル環境と CI で結果を統一する）。上げる場合はここのみを修正する。
$version = '1.25.0'
# Pester は Windows に同梱されている 3.4 を使用しない（フックのテストは Pester 5 で記述する）。
$pesterVersion = '5.7.1'

if ($Mode -eq 'Install') {
	if ($Module -contains 'PSScriptAnalyzer') {
		Install-Module PSScriptAnalyzer -RequiredVersion $version -Scope CurrentUser -Force
	}
	if ($Module -contains 'Pester') {
		# Windows に同梱されている Pester 3.4 と発行元が異なるため、発行元の確認を省略して並存させて
		# インストールする。
		Install-Module Pester -RequiredVersion $pesterVersion -Scope CurrentUser -Force -SkipPublisherCheck
	}
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

# Lint は、ファイルごとに子の pwsh（-Mode LintFile）で検査する。PSScriptAnalyzer は、同じ
# ファイルでも時折解析器の中で異常終了する（NullReferenceException・PowerShell の実行時の
# 「An item with the same key has already been added」でプロセスごと終了する。指摘ではなく、
# 再実行すると通過する。PSScriptAnalyzer #1351・#1511）。プロセスごと異常終了するとスクリプトの
# 中では捕捉できないため、子が指摘（1）でも通過（0）でもなく終了した場合のみ、その旨を書き込んでから
# 3 回まで再実行する。指摘は再実行しない。
if ($Mode -eq 'Lint') {
	$pwsh = (Get-Process -Id $PID).Path
	$status = 0
	foreach ($file in $Path) {
		for ($attempt = 1; ; $attempt++) {
			& $pwsh -NoProfile -File $PSCommandPath -Mode LintFile $file
			$code = $LASTEXITCODE
			# 0 と 1 の他は異常終了とみなす（Windows で .NET の例外のまま終了した pwsh は負の
			# 終了コードを返す）。
			if ($code -eq 0 -or $code -eq 1 -or $attempt -ge 3) {
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
# 書き戻す際は BOM を付ける（.editorconfig）。
$utf8 = [System.Text.UTF8Encoding]::new($true)

# 子プロセス（LintFile）では、解析器の中の失敗（停止する誤り）を 3 で終了させ、指摘（1）と区別する。
# 他のモードでは、従来通り停止する（break は誤りをそのまま投げる）。exit ではなく
# [Environment]::Exit でプロセスを直ちに終了させる（exit は後処理で、異常終了した解析器のまだ
# 動作しているスレッドを待機し、15 分ほど戻らないことがあった）。
trap {
	if ($Mode -ne 'LintFile') {
		break
	}
	[Console]::Error.WriteLine(('{0}: {1}' -f ($Path -join ' '), $_.Exception))
	[Environment]::Exit(3)
}

$status = 0
foreach ($file in $Path) {
	if ($Mode -eq 'LintFile') {
		# PSUseCorrectCasing は検査から除外する。整形（Invoke-Formatter）が同じ設定で適用する
		# 規則であるため、大文字と小文字の違いは format.sh の確認が検出する。検査では、この規則が
		# コマンドの情報を参照する箇所で時折異常終了する（CommandInfo.get_Parameters の
		# NullReferenceException）。
		$found = Invoke-ScriptAnalyzer -Path $file -Settings $settings -ExcludeRule PSUseCorrectCasing
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
	# else を記述しない（整形の規則が } と else の間で改行し、else を空白で字下げするため）。
	if ($Mode -eq 'Fix') {
		[System.IO.File]::WriteAllText($file, $formatted, $utf8)
		continue
	}
	$file
	$status = 1
}
exit $status
