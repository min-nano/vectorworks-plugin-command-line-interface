# The first-time script for Windows (docs/plugin/install-and-update.md). It only fetches the CLI
# and runs "vw2026 install cli@<tag>" (or "uninstall cli"); the steps, the Path, and the
# decisions belong to install and to the hooks of cli. It runs on Windows PowerShell 5.1.
#
# This file is ASCII only, and the release uploads it without the BOM. Invoke-RestMethod in
# Windows PowerShell 5.1 decodes a body without a charset (a release asset) as ISO-8859-1, so a
# BOM or any non-ASCII text would reach Invoke-Expression garbled.
#
#   irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1 | iex
#   & ([scriptblock]::Create((irm .../get-vw2026.ps1))) cli@<tag> --plugins-dir <Plug-Ins>
#   & ([scriptblock]::Create((irm .../get-vw2026.ps1))) uninstall cli
#
# The arguments come in $args: under Invoke-Expression a param block of the script is not
# bound. The body runs in a script block so that its variables and $ErrorActionPreference stay
# out of the session of the user. It does not call exit, which under Invoke-Expression closes the
# PowerShell of the user: the exit code of install or uninstall is left in $LASTEXITCODE, and a
# failure before the CLI runs stops the script with an exception.
& {
	param([string[]] $Arguments)

	$ErrorActionPreference = 'Stop'
	# The progress bar of Windows PowerShell 5.1 slows the download down a lot.
	$ProgressPreference = 'SilentlyContinue'
	$repo = 'https://github.com/min-nano/vectorworks-plugin-command-line-interface'

	# Look at the positional arguments in order. The argument after a flag that takes a value
	# (--plugins-dir, --registry) is its value; --name=value is one argument, a flag here.
	$uninstall = $false
	$tag = ''
	$positional = 0
	$skip = $false
	foreach ($arg in $Arguments) {
		if ($skip) {
			$skip = $false
			continue
		}
		if ($arg -ceq '--plugins-dir' -or $arg -ceq '--registry') {
			$skip = $true
			continue
		}
		if ($arg.StartsWith('-')) {
			continue
		}
		$positional++
		if ($positional -eq 1 -and $arg -ceq 'uninstall') {
			$uninstall = $true
			continue
		}
		if ($uninstall) {
			# This is the way out when "uninstall cli" stopped in its hook and the CLI is gone, so it
			# takes cli only.
			if ($positional -ne 2 -or $arg -cne 'cli') {
				throw 'get-vw2026: only "uninstall cli" is accepted'
			}
			continue
		}
		# The script installs cli, and no other name.
		if (-not $arg.StartsWith('cli@', [StringComparison]::Ordinal)) {
			throw "get-vw2026: only cli@<tag> is accepted: $arg"
		}
		if ($tag) {
			throw 'get-vw2026: give cli@<tag> only once'
		}
		$tag = $arg.Substring(4)
		# Only the characters of a tag (docs/registry.md), as it goes into the URL.
		if ($tag -cnotmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') {
			throw "get-vw2026: not a valid tag: $tag"
		}
	}
	$argv = @($Arguments)
	if ($uninstall) {
		if ($positional -ne 2) {
			throw 'get-vw2026: only "uninstall cli" is accepted'
		}
		# The CLI of latest goes on from the hook of the stopped uninstall.
		$tag = 'latest'
	}
	elseif (-not $tag) {
		$tag = 'latest'
		$argv += 'cli@latest'
	}

	# The target is windows-x64 (Vectorworks runs as x64 on Windows on ARM too). The URL has the
	# shape of the rules of cli in the default registry; the script does not read the manifest.
	if ($tag -ceq 'latest') {
		$url = "$repo/releases/latest/download/cli-windows-x64.zip"
	}
	else {
		$url = "$repo/releases/download/$tag/cli-windows-x64.zip"
	}

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

		& $exe $argv
		if ($LASTEXITCODE -eq 6) {
			[Console]::Error.WriteLine('get-vw2026: Vectorworks is running. Quit Vectorworks and run this script again.')
		}
	}
	finally {
		# A failure to remove the temporary folder does not hide the outcome of the CLI.
		Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
	}
} @($args | ForEach-Object { [string] $_ })
