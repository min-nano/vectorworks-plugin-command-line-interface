# cli の uninstall のフック（Windows）。利用者の Path（HKCU\Environment）から <Plug-Ins>\cli\bin と
# 一致する項目をすべて除き、標準出力に {"path":…,"left":[…]} を 1 つだけ書く
# （docs/plugin/install-and-update.md「`uninstall` のフック」）。path は lib.ps1 の Uninstall-PathEntry
# （removed・none・failed）で、failed なら残った項目を left に載せる。
#
# フォルダは <Work>\old\cli\ へ移したあとで走る（VW2026_HOOK_PLUGIN_DIR は移した先）ので、除く項目は
# VW2026_HOOK_INSTALL_DIR から決める。消し始めたあとは失敗にしない（失敗にすると CLI がフォルダを
# 戻し、プラグインは残るのに Path の項目だけが消えた状態になる）。
#
# -Store はテストが偽の置き場所を渡すためのもの。vw2026 uninstall は渡さない（利用者の Path を読み書きする）。
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
if ($null -eq $Store) {
	$Store = Get-RegistryPathStore
}

# ここから先は失敗にしない。思わぬ例外も failed として left に載せる。
$state = 'failed'
try { $state = Uninstall-PathEntry $Store $bin } catch { Write-HookException 'cannot update Path' $_ }
$left = ''
if ($state -eq 'failed') {
	$left = ConvertTo-HookJson ('HKCU\Environment\Path: ' + $bin)
}
Write-HookOutput ('{"path":' + (ConvertTo-HookJson $state) + ',"left":[' + $left + ']}')
exit 0
