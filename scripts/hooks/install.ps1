# cli の install のフック（Windows）。利用者の Path（HKCU\Environment）に <Plug-Ins>\cli\bin を
# 追加し、標準出力に {"path":…} を 1 つのみ書き込む（docs/plugin/install-and-update.md「PATH」）。
# 値は lib.ps1 の Install-PathEntry（added・exists・conflict・failed）。
#
# 何度実行しても同じ結果になる。Path の失敗ではフックを失敗させない（プラグインは動作する）。
# 失敗させるのは、何も変更する前の確認（環境変数・CLI の存在）のみ。
#
# -Store はテストが擬似の配置場所を渡すためのもの。vw2026 install は渡さない（利用者の Path を
# 読み書きする）。
param([hashtable] $Store)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
. (Join-Path $PSScriptRoot 'lib.ps1')
Initialize-HookConsole

$target = Get-HookTarget
if ($null -eq $target) {
	exit 1
}
$bin = $target.Bin
# 2025 の CLI が実行した場合は vw2025.exe（lib.ps1 の Get-HookYear）。
$cli = $bin + '\vw' + $target.Year + '.exe'
if (-not (Test-Path -LiteralPath $cli -PathType Leaf)) {
	Write-HookError ('the CLI is missing: ' + $cli)
	exit 1
}
if ($null -eq $Store) {
	$Store = Get-RegistryPathStore
}

$state = Install-PathEntry $Store $bin
Write-HookOutput ('{"path":' + (ConvertTo-HookJson $state) + '}')
exit 0
