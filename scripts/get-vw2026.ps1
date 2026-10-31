# The first-time script for Windows (docs/plugin/install-and-update.md). It only fetches the CLI
# of the release that it came from and runs "vw2026 install cli@<the tag of the release>" (or
# "uninstall cli"); the steps, the Path, and the decisions belong to install and to the hooks of
# cli. The tag is that of the release the script came from; for another tag, fetch the script of
# that tag. It runs on Windows PowerShell 5.1.
#
# This file is ASCII only, and the release uploads it without the BOM. Invoke-RestMethod in
# Windows PowerShell 5.1 decodes a body without a charset (a release asset) as ISO-8859-1, so a
# BOM or any non-ASCII text would reach Invoke-Expression garbled.
#
#   irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1 | iex
#   irm .../releases/download/<tag>/get-vw2026.ps1 | iex
#   & ([scriptblock]::Create((irm .../get-vw2026.ps1))) --plugins-dir <Plug-Ins>
#   & ([scriptblock]::Create((irm .../releases/download/<tag>/get-vw2026.ps1))) uninstall
#
# The arguments come in $args: under Invoke-Expression a param block of the script is not
# bound. The body runs in a script block so that its variables and $ErrorActionPreference stay
# out of the session of the user. It does not call exit, which under Invoke-Expression closes the
# PowerShell of the user: the exit code of install or uninstall is left in $LASTEXITCODE, and a
# failure before the CLI runs stops the script with an exception.
& {
	# $LASTEXITCODE is set like a native command would set it, for the caller to read.
	[Diagnostics.CodeAnalysis.SuppressMessageAttribute('PSAvoidGlobalVars', '')]
	param([string[]] $Arguments)

	$ErrorActionPreference = 'Stop'
	# The progress bar of Windows PowerShell 5.1 slows the download down a lot.
	$ProgressPreference = 'SilentlyContinue'
	$repo = 'https://github.com/min-nano/vectorworks-plugin-command-line-interface'

	# The release replaces the tag with its own when it publishes the script
	# (docs/plugin/build-and-release.md). The script of any release installs and uninstalls the cli
	# of that release.
	$tag = '@VW2026_TAG@'
	# Only the characters of a tag (docs/registry.md), as it goes into the URL; this also refuses
	# the script of the repository, which no release has replaced.
	if ($tag -cnotmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') {
		throw "get-vw2026: this script is not the one a release published; use $repo/releases/latest/download/get-vw2026.ps1"
	}

	# The only positional argument is uninstall (or uninstall cli), and the only flag is
	# --plugins-dir (or --plugins-dir=<dir>). There is no --registry: install looks the name up in the
	# default registry first, which has cli, so no other registry is used; uninstall reads no
	# registry. The flag goes after the command of the CLI wherever it is written (vw2026 takes no
	# flag before the command).
	$uninstall = $false
	$positional = 0
	$skip = $false
	$flags = @(foreach ($arg in $Arguments) {
			if ($skip) {
				$skip = $false
				$arg
				continue
			}
			if ($arg -ceq '--plugins-dir' -or $arg.StartsWith('--plugins-dir=', [StringComparison]::Ordinal)) {
				$skip = ($arg -ceq '--plugins-dir')
				$arg
				continue
			}
			if ($arg.StartsWith('-')) {
				throw "get-vw2026: unknown flag: $arg (this script takes only --plugins-dir)"
			}
			$positional++
			if ($positional -eq 1 -and $arg -ceq 'uninstall') {
				$uninstall = $true
				continue
			}
			# "uninstall cli" is the same (the form of "vw2026 uninstall cli").
			if ($positional -eq 2 -and $uninstall -and $arg -ceq 'cli') {
				continue
			}
			throw "get-vw2026: unexpected argument: $arg (this script installs cli@$tag; for another tag, use $repo/releases/download/<tag>/get-vw2026.ps1)"
		})
	if ($uninstall) {
		$argv = @('uninstall', 'cli') + $flags
	}
	else {
		$argv = @('install', "cli@$tag") + $flags
	}

	# The target is windows-x64 (Vectorworks runs as x64 on Windows on ARM too). The URL has the
	# shape of the rules of cli in the default registry; the script does not read the manifest.
	$url = "$repo/releases/download/$tag/cli-windows-x64.zip"

	# Windows PowerShell 5.1 may not use TLS 1.2 by default.
	[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
	$temp = Join-Path ([IO.Path]::GetTempPath()) ('get-vw2026-' + [Guid]::NewGuid().ToString('N'))
	[void] (New-Item -ItemType Directory -Path $temp)
	try {
		$zip = Join-Path $temp 'cli.zip'
		[Console]::Error.WriteLine("get-vw2026: downloading $url")
		Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $zip
		# Take out the CLI only.
		Add-Type -AssemblyName System.IO.Compression.FileSystem
		$exe = Join-Path $temp 'vw2026.exe'
		$archive = [IO.Compression.ZipFile]::OpenRead($zip)
		try {
			$entry = $archive.GetEntry('bin/vw2026.exe')
			if (-not $entry) {
				throw "get-vw2026: no bin/vw2026.exe in $url"
			}
			[IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $exe)
		}
		finally {
			$archive.Dispose()
		}

		# Windows PowerShell 5.1 quotes the arguments of a native command its own way: it drops an
		# empty argument, and in a value with a space a trailing \ escapes the closing quote. So the
		# command line is built here by the rules of CommandLineToArgvW. The standard output of the
		# CLI (one line of JSON in UTF-8) is passed on; the standard error goes to the console.
		$quoted = foreach ($arg in $argv) {
			if ($arg.Length -gt 0 -and $arg -notmatch '[\s"]') {
				$arg
				continue
			}
			$escaped = [regex]::Replace($arg, '(\\*)"', { param($match) $match.Groups[1].Value * 2 + '\"' })
			'"' + [regex]::Replace($escaped, '(\\+)$', { param($match) $match.Groups[1].Value * 2 }) + '"'
		}
		$info = [Diagnostics.ProcessStartInfo]::new($exe)
		$info.Arguments = $quoted -join ' '
		$info.UseShellExecute = $false
		$info.RedirectStandardOutput = $true
		$info.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
		$process = [Diagnostics.Process]::Start($info)
		$output = $process.StandardOutput.ReadToEnd()
		$process.WaitForExit()
		# The exit code of install or uninstall is left in $LASTEXITCODE (as for a native command).
		$global:LASTEXITCODE = $process.ExitCode
		if ($output) {
			$output.TrimEnd("`r", "`n")
		}
		if ($LASTEXITCODE -eq 6) {
			[Console]::Error.WriteLine('get-vw2026: Vectorworks is running. Quit Vectorworks and run this script again.')
		}
	}
	finally {
		# A failure to remove the temporary folder does not hide the outcome of the CLI.
		Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
	}
} @($args | ForEach-Object { [string] $_ })
