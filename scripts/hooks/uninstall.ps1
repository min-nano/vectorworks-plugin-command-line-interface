# cli の uninstall のフック（Windows）。利用者の Path（HKCU\Environment）から <Plug-Ins>\cli\bin と
# 一致する項目を全て除去し、標準出力に {"path":…,"left":[…]} を 1 つのみ書き込む
# （docs/plugin/install-and-update.md「`uninstall` のフック」）。path は lib.ps1 の
# Uninstall-PathEntry（removed・none・failed）で、failed ならば残存した項目を left に記載する。
#
# フォルダは <Work>\old\cli\ へ移動した後で実行される（VW2026_HOOK_PLUGIN_DIR は移動先）ため、
# 除去する項目は VW2026_HOOK_INSTALL_DIR から決定する。削除を開始した後は失敗させない（失敗させると
# CLI がフォルダを元に戻し、プラグインは残存するのに Path の項目のみが削除された状態になる）。
#
# -Store はテストが擬似の配置場所を渡すためのもの。vw2026 uninstall は渡さない（利用者の Path を
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
if ($null -eq $Store) {
	$Store = Get-RegistryPathStore
}

# ここから先は失敗させない。予期しない例外も failed として left に記載する。
$state = 'failed'
try { $state = Uninstall-PathEntry $Store $bin } catch { Write-HookException 'cannot update Path' $_ }
$left = ''
if ($state -eq 'failed') {
	$left = ConvertTo-HookJson ('HKCU\Environment\Path: ' + $bin)
}
Write-HookOutput ('{"path":' + (ConvertTo-HookJson $state) + ',"left":[' + $left + ']}')
exit 0
