# テストの擬似の配置場所（scripts/hooks/lib.ps1 の Get-RegistryPathStore と同じ形式）。利用者の
# Path を読み書きせず、State に値と、書き込んだ回数・通知した回数を保持する。Hooks.Tests.ps1 と、
# それが実行する子プロセスが読み込む。
function Get-FakePathStore {
	param(
		[string] $Value = '',
		[string] $Kind = 'ExpandString',
		# 値が無い
		[switch] $Missing,
		[switch] $FailRead,
		[switch] $FailWrite,
		[switch] $FailNotify
	)
	$state = @{
		Exists     = -not $Missing
		Kind       = $Kind
		Value      = $Value
		Writes     = 0
		Notified   = 0
		FailRead   = [bool] $FailRead
		FailWrite  = [bool] $FailWrite
		FailNotify = [bool] $FailNotify
	}
	if ($Missing) {
		$state.Kind = ''
		$state.Value = ''
	}
	return @{
		State  = $state
		Read   = {
			if ($state.FailRead) { throw 'cannot read' }
			return @{ Exists = $state.Exists; Kind = $state.Kind; Value = $state.Value }
		}.GetNewClosure()
		Write  = {
			param([string] $Value, [string] $Kind)
			if ($state.FailWrite) { throw 'cannot write' }
			$state.Exists = $true
			$state.Kind = $Kind
			$state.Value = $Value
			$state.Writes += 1
		}.GetNewClosure()
		Notify = {
			if ($state.FailNotify) { throw 'cannot notify' }
			$state.Notified += 1
		}.GetNewClosure()
	}
}
