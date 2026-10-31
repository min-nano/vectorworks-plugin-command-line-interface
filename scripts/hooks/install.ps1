# cli の install のフック（Windows）。利用者の Path（HKCU\Environment）に <Plug-Ins>\cli\bin を足し、
# 標準出力に {"path":…} を 1 つだけ書く（docs/plugin/install-and-update.md「PATH」）。値は
# lib.ps1 の Install-PathEntry（added・exists・conflict・failed）。
#
# 何度走らせても同じ結果になる。Path の失敗ではフックを失敗にしない（プラグインは動く）。失敗に
# するのは、何も変える前の確かめ（環境変数・CLI があること）だけ。
#
# -Store はテストが偽の置き場所を渡すためのもの。vw2026 install は渡さない（利用者の Path を読み書きする）。
param([hashtable] $Store)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
. (Join-Path $PSScriptRoot 'lib.ps1')
Initialize-HookConsole

$installDir = Get-HookInstallDir
if ($null -eq $installDir) {
	exit 1
}
$bin = $installDir + '\bin'
$cli = $bin + '\vw2026.exe'
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
