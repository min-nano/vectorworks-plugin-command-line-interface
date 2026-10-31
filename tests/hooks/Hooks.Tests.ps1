# cli のフック（scripts/hooks/*.ps1）のテスト（docs/plugin/install-and-update.md「テスト」）。Pester 5 で、
# Windows PowerShell 5.1（フックが走るのと同じもの）で走らせる。版は scripts/powershell.ps1 が固定する。
#
#   powershell -NoProfile -File scripts/powershell.ps1 -Mode Test tests/hooks/Hooks.Tests.ps1
#
# 利用者の Path には触れない。lib.ps1 の関数は偽の置き場所（FakePathStore.ps1）で確かめ、install.ps1・
# uninstall.ps1 の全体は、偽の置き場所を -Store に渡す子のプロセスで走らせて、標準出力のバイト列を見る。

BeforeAll {
	$script:hooks = Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)) 'scripts') 'hooks'
	$script:fake = Join-Path $PSScriptRoot 'FakePathStore.ps1'
	. (Join-Path $script:hooks 'lib.ps1')
	. $script:fake

	# ConvertTo-Quoted は文字列を PowerShell の単一引用符の文字列にする。
	function ConvertTo-Quoted {
		param([string] $Text)
		return "'" + $Text.Replace("'", "''") + "'"
	}

	# Invoke-Hook はフック（install・uninstall）を子のプロセスで走らせる。偽の置き場所は子の中で
	# $StoreArguments（Get-FakePathStore の引数）から作り、走らせたあとの State を返す。
	# $InstallDir が空なら VW2026_HOOK_INSTALL_DIR を渡さない。
	function Invoke-Hook {
		param([string] $Hook, [string] $InstallDir, [string] $PluginDir, [string] $StoreArguments)
		$stateFile = Join-Path $TestDrive ([Guid]::NewGuid().ToString() + '.json')
		$hookScript = Join-Path $script:hooks ($Hook + '.ps1')
		$command = @(
			"`$ErrorActionPreference = 'Stop'"
			". $(ConvertTo-Quoted $script:fake)"
			"`$store = Get-FakePathStore $StoreArguments"
			"& $(ConvertTo-Quoted $hookScript) -Store `$store"
			"`$code = `$LASTEXITCODE"
			"[IO.File]::WriteAllText($(ConvertTo-Quoted $stateFile), (ConvertTo-Json `$store.State -Compress))"
			"exit `$code"
		) -join "`n"
		$encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
		$info = [Diagnostics.ProcessStartInfo]::new((Get-Process -Id $PID).Path)
		$info.Arguments = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -EncodedCommand ' + $encoded
		$info.UseShellExecute = $false
		$info.RedirectStandardOutput = $true
		$info.RedirectStandardError = $true
		$info.EnvironmentVariables.Remove('VW2026_HOOK_INSTALL_DIR')
		if (-not [string]::IsNullOrEmpty($InstallDir)) {
			$info.EnvironmentVariables['VW2026_HOOK_INSTALL_DIR'] = $InstallDir
		}
		$info.EnvironmentVariables['VW2026_HOOK_PLUGIN_DIR'] = $PluginDir
		$process = [Diagnostics.Process]::Start($info)
		$stderr = $process.StandardError.ReadToEndAsync()
		$stdout = [IO.MemoryStream]::new()
		$process.StandardOutput.BaseStream.CopyTo($stdout)
		$process.WaitForExit()
		$state = $null
		if (Test-Path -LiteralPath $stateFile) {
			$state = [IO.File]::ReadAllText($stateFile) | ConvertFrom-Json
		}
		return @{
			Code   = $process.ExitCode
			Stdout = $stdout.ToArray()
			Stderr = $stderr.Result
			State  = $state
		}
	}

	# Initialize-PluginDir は <Plug-Ins>\cli\bin\vw2026.exe を作り、<Plug-Ins>\cli を返す（日本語を含むパス）。
	function Initialize-PluginDir {
		$dir = Join-Path (Join-Path $TestDrive ([Guid]::NewGuid().ToString() + ' 日本語')) 'cli'
		New-Item -ItemType Directory -Path ($dir + '\bin') -Force | Out-Null
		New-Item -ItemType File -Path ($dir + '\bin\vw2026.exe') | Out-Null
		return $dir
	}

	# Get-Ascii は標準出力のバイト列を文字列にする。ASCII の外のバイトがあれば失敗にする（出力は
	# ConvertTo-HookJson で ASCII だけにしている。UTF-8 としても読める）。
	function Get-Ascii {
		param([byte[]] $Bytes)
		@($Bytes | Where-Object { $_ -ge 0x80 }).Count | Should -Be 0
		return [Text.Encoding]::ASCII.GetString($Bytes)
	}
}

Describe 'Test-PathEntry' {
	It 'ignores the case, the spaces around, one pair of quotes, and the trailing backslash' {
		Test-PathEntry ' "C:\Users\U\Plug-Ins\CLI\BIN\" ' 'c:\users\u\plug-ins\cli\bin' | Should -BeTrue
		Test-PathEntry 'C:\x\bin\\' 'C:\x\bin' | Should -BeTrue
	}
	It 'does not expand variables' {
		Test-PathEntry '%APPDATA%\cli\bin' 'C:\Users\U\AppData\Roaming\cli\bin' | Should -BeFalse
	}
	It 'removes only one pair of quotes' {
		Test-PathEntry '""C:\x""' 'C:\x' | Should -BeFalse
	}
	It 'does not match an empty entry or another directory' {
		Test-PathEntry '' 'C:\x' | Should -BeFalse
		Test-PathEntry 'C:\x\bin2' 'C:\x\bin' | Should -BeFalse
	}
	It 'matches the same directory written another way, also below a folder that is gone' {
		$real = Join-Path $TestDrive 'real'
		New-Item -ItemType Directory -Path (Join-Path $real 'cli\bin') -Force | Out-Null
		$junction = Join-Path $TestDrive 'junction'
		New-Item -ItemType Junction -Path $junction -Target $real | Out-Null
		Test-PathEntry "$junction\cli\bin" "$real\cli\bin" | Should -BeTrue
		Test-PathEntry "$junction\gone\bin" "$real\gone\bin" | Should -BeTrue
		Test-PathEntry "$junction\gone\bin" "$real\gone\bin2" | Should -BeFalse
		Test-PathEntry $junction "$real\cli" | Should -BeFalse
	}
	It 'does not match a relative entry or one that is not a path' {
		Test-PathEntry 'cli\bin' (Join-Path $PWD 'cli\bin') | Should -BeFalse
		Test-PathEntry 'C:\a|b' 'C:\a' | Should -BeFalse
	}
}

Describe 'Get-PathWithEntry' {
	It 'adds the entry without an empty one' {
		Get-PathWithEntry '' 'D' | Should -BeExactly 'D'
		Get-PathWithEntry 'C:\a' 'D' | Should -BeExactly 'C:\a;D'
		Get-PathWithEntry 'C:\a;' 'D' | Should -BeExactly 'C:\a;D'
	}
	It 'keeps the order, the empty entries and the spaces' {
		Get-PathWithEntry ' C:\a ;;C:\b' 'D' | Should -BeExactly ' C:\a ;;C:\b;D'
	}
}

Describe 'Get-PathWithoutEntry' {
	It 'removes the entry with the separator before it' {
		Get-PathWithoutEntry 'C:\a;D;C:\b' 'D' | Should -BeExactly 'C:\a;C:\b'
		Get-PathWithoutEntry 'C:\a;D' 'D' | Should -BeExactly 'C:\a'
		Get-PathWithoutEntry 'C:\a;D;' 'D' | Should -BeExactly 'C:\a;'
		Get-PathWithoutEntry 'C:\a;;D' 'D' | Should -BeExactly 'C:\a;'
	}
	It 'removes the first entry with the separator after it' {
		Get-PathWithoutEntry 'D;C:\a' 'D' | Should -BeExactly 'C:\a'
	}
	It 'removes every matching entry' {
		Get-PathWithoutEntry 'C:\a;d; "D\" ;C:\b' 'D' | Should -BeExactly 'C:\a;C:\b'
	}
	It 'leaves nothing when only the entry was there' {
		Get-PathWithoutEntry 'D' 'D' | Should -BeExactly ''
		Get-PathWithoutEntry 'D;' 'D' | Should -BeExactly ''
		Get-PathWithoutEntry 'D;D' 'D' | Should -BeExactly ''
	}
	It 'does not bring back the trailing separator after adding and removing' {
		Get-PathWithoutEntry (Get-PathWithEntry 'C:\a;' 'D') 'D' | Should -BeExactly 'C:\a'
	}
	It 'keeps the value without the entry' {
		Get-PathWithoutEntry '%USERPROFILE%\x;;C:\b' 'D' | Should -BeExactly '%USERPROFILE%\x;;C:\b'
	}
}

Describe 'ConvertTo-HookJson' {
	It 'escapes the quotes, the backslashes, the control characters and the characters outside ASCII' {
		ConvertTo-HookJson 'a"b\c' | Should -BeExactly '"a\"b\\c"'
		ConvertTo-HookJson ("x`ty") | Should -BeExactly '"x\u0009y"'
		ConvertTo-HookJson '日本' | Should -BeExactly '"\u65e5\u672c"'
		ConvertTo-HookJson '' | Should -BeExactly '""'
	}
}

Describe 'Install-PathEntry' {
	It 'makes the value as REG_EXPAND_SZ when there is none' {
		$store = Get-FakePathStore -Missing
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'added'
		$store.State.Kind | Should -BeExactly 'ExpandString'
		$store.State.Value | Should -BeExactly 'C:\p\cli\bin'
		$store.State.Notified | Should -Be 1
	}
	It 'keeps REG_SZ' {
		$store = Get-FakePathStore -Kind String -Value 'C:\vw2026-probe-sz'
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'added'
		$store.State.Kind | Should -BeExactly 'String'
		$store.State.Value | Should -BeExactly 'C:\vw2026-probe-sz;C:\p\cli\bin'
	}
	It 'keeps REG_EXPAND_SZ and does not expand the entries' {
		$store = Get-FakePathStore -Kind ExpandString -Value '%USERPROFILE%\vw2026-path-probe;'
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'added'
		$store.State.Kind | Should -BeExactly 'ExpandString'
		$store.State.Value | Should -BeExactly '%USERPROFILE%\vw2026-path-probe;C:\p\cli\bin'
	}
	It 'does nothing when the entry is there in another spelling' {
		$store = Get-FakePathStore -Value 'C:\a; "c:\P\CLI\bin\" '
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'exists'
		$store.State.Writes | Should -Be 0
		$store.State.Notified | Should -Be 0
	}
	It 'does not rewrite a value of another type' {
		$store = Get-FakePathStore -Kind Binary
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'conflict'
		$store.State.Writes | Should -Be 0
	}
	It 'reports a failure to read or write' {
		Install-PathEntry (Get-FakePathStore -FailRead) 'C:\p\cli\bin' | Should -BeExactly 'failed'
		Install-PathEntry (Get-FakePathStore -FailWrite) 'C:\p\cli\bin' | Should -BeExactly 'failed'
	}
	It 'ignores a failure to notify' {
		$store = Get-FakePathStore -Value 'C:\a' -FailNotify
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'added'
		$store.State.Value | Should -BeExactly 'C:\a;C:\p\cli\bin'
	}
	It 'gives the same value when run twice' {
		$store = Get-FakePathStore -Value 'C:\a'
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'added'
		Install-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'exists'
		$store.State.Value | Should -BeExactly 'C:\a;C:\p\cli\bin'
	}
}

Describe 'Uninstall-PathEntry' {
	It 'removes every matching entry and keeps the type' {
		$store = Get-FakePathStore -Kind String -Value 'C:\a;C:\p\cli\bin; "C:\P\CLI\BIN\" '
		Uninstall-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'removed'
		$store.State.Kind | Should -BeExactly 'String'
		$store.State.Value | Should -BeExactly 'C:\a'
		$store.State.Notified | Should -Be 1
	}
	It 'keeps the entries written with variables' {
		$store = Get-FakePathStore -Value '%USERPROFILE%\vw2026-path-probe;C:\p\cli\bin'
		Uninstall-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'removed'
		$store.State.Kind | Should -BeExactly 'ExpandString'
		$store.State.Value | Should -BeExactly '%USERPROFILE%\vw2026-path-probe'
	}
	It 'deletes the value when nothing is left' {
		$store = Get-FakePathStore -Value 'C:\p\cli\bin;'
		Uninstall-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'removed'
		$store.State.Exists | Should -BeFalse
	}
	It 'does nothing without the entry' {
		$missing = Get-FakePathStore -Missing
		Uninstall-PathEntry $missing 'C:\p\cli\bin' | Should -BeExactly 'none'
		$other = Get-FakePathStore -Value 'C:\a;;C:\p\cli\bin2'
		Uninstall-PathEntry $other 'C:\p\cli\bin' | Should -BeExactly 'none'
		$binary = Get-FakePathStore -Kind Binary
		Uninstall-PathEntry $binary 'C:\p\cli\bin' | Should -BeExactly 'none'
		$missing.State.Writes + $other.State.Writes + $binary.State.Writes | Should -Be 0
	}
	It 'reports a failure to read or write without throwing' {
		Uninstall-PathEntry (Get-FakePathStore -FailRead) 'C:\p\cli\bin' | Should -BeExactly 'failed'
		$store = Get-FakePathStore -Value 'C:\p\cli\bin' -FailWrite
		Uninstall-PathEntry $store 'C:\p\cli\bin' | Should -BeExactly 'failed'
		$store.State.Value | Should -BeExactly 'C:\p\cli\bin'
	}
}

Describe 'install.ps1' {
	It 'adds the entry and writes one JSON object in ASCII, for a path in Japanese' {
		$dir = Initialize-PluginDir
		$r = Invoke-Hook -Hook install -InstallDir $dir -PluginDir $dir -StoreArguments "-Value 'C:\a'"
		$r.Code | Should -Be 0
		Get-Ascii $r.Stdout | Should -BeExactly "{`"path`":`"added`"}`n"
		$r.State.Value | Should -BeExactly ('C:\a;' + $dir + '\bin')
		$r.State.Notified | Should -Be 1
	}
	It 'reports exists for the entry already there' {
		$dir = Initialize-PluginDir
		$store = '-Value ' + (ConvertTo-Quoted ($dir + '\bin'))
		$r = Invoke-Hook -Hook install -InstallDir ($dir + '\') -PluginDir $dir -StoreArguments $store
		$r.Code | Should -Be 0
		Get-Ascii $r.Stdout | Should -BeExactly "{`"path`":`"exists`"}`n"
		$r.State.Writes | Should -Be 0
	}
	It 'fails without changing anything when the CLI is missing' {
		$dir = Initialize-PluginDir
		Remove-Item -LiteralPath ($dir + '\bin\vw2026.exe')
		$r = Invoke-Hook -Hook install -InstallDir $dir -PluginDir $dir -StoreArguments "-Value 'C:\a'"
		$r.Code | Should -Be 1
		$r.Stdout.Length | Should -Be 0
		$r.State.Writes | Should -Be 0
	}
	It 'fails without changing anything when VW2026_HOOK_INSTALL_DIR is missing or relative' {
		$dir = Initialize-PluginDir
		foreach ($installDir in @('', 'cli')) {
			$r = Invoke-Hook -Hook install -InstallDir $installDir -PluginDir $dir -StoreArguments "-Value 'C:\a'"
			$r.Code | Should -Be 1
			$r.Stdout.Length | Should -Be 0
			$r.State.Writes | Should -Be 0
		}
	}
}

Describe 'uninstall.ps1' {
	It 'removes the entry by VW2026_HOOK_INSTALL_DIR while the folder is moved away' {
		$dir = Initialize-PluginDir
		$moved = Join-Path $TestDrive ([Guid]::NewGuid().ToString() + ' old')
		Move-Item -LiteralPath $dir -Destination $moved
		$store = '-Value ' + (ConvertTo-Quoted ('C:\a;' + $dir + '\bin'))
		$r = Invoke-Hook -Hook uninstall -InstallDir $dir -PluginDir $moved -StoreArguments $store
		$r.Code | Should -Be 0
		Get-Ascii $r.Stdout | Should -BeExactly "{`"path`":`"removed`",`"left`":[]}`n"
		$r.State.Value | Should -BeExactly 'C:\a'
	}
	It 'reports none without the entry' {
		$dir = Initialize-PluginDir
		$r = Invoke-Hook -Hook uninstall -InstallDir $dir -PluginDir $dir -StoreArguments "-Value 'C:\a'"
		$r.Code | Should -Be 0
		Get-Ascii $r.Stdout | Should -BeExactly "{`"path`":`"none`",`"left`":[]}`n"
	}
	It 'does not fail when it cannot write, and lists the entry in left' {
		$dir = Initialize-PluginDir
		$store = '-FailWrite -Value ' + (ConvertTo-Quoted ($dir + '\bin'))
		$r = Invoke-Hook -Hook uninstall -InstallDir $dir -PluginDir $dir -StoreArguments $store
		$r.Code | Should -Be 0
		$left = ConvertTo-HookJson ('HKCU\Environment\Path: ' + $dir + '\bin')
		Get-Ascii $r.Stdout | Should -BeExactly ("{`"path`":`"failed`",`"left`":[" + $left + "]}`n")
	}
	It 'fails without changing anything when VW2026_HOOK_INSTALL_DIR is missing' {
		$dir = Initialize-PluginDir
		$store = '-Value ' + (ConvertTo-Quoted ($dir + '\bin'))
		$r = Invoke-Hook -Hook uninstall -InstallDir '' -PluginDir $dir -StoreArguments $store
		$r.Code | Should -Be 1
		$r.Stdout.Length | Should -Be 0
		$r.State.Writes | Should -Be 0
	}
}
