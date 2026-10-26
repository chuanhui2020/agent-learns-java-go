# Java 25 source launcher for Windows PowerShell 5.1 and PowerShell 7.
# Keep this script ASCII so Windows PowerShell can read it without a BOM.
# Usage: .\bin\jrun.ps1 [JVM options] path/to/File.java [program arguments]
# Quote JVM options, for example: '-Dmessage=hello world'.
$ErrorActionPreference = 'Stop'

if ($args.Count -eq 0 -or $args[0] -in @('--help', '-h')) {
    Write-Output 'Usage: .\bin\jrun.ps1 [JVM options] path/to/File.java [program arguments]'
    Write-Output 'Requires JDK 25. Quote JVM options and put them before the source file.'
    if ($args.Count -eq 0) { exit 2 }
    exit 0
}

try {
    if ($env:JAVA_HOME) {
        $javaCommand = Join-Path $env:JAVA_HOME 'bin/java.exe'
        if (-not (Test-Path -LiteralPath $javaCommand -PathType Leaf)) {
            throw "JAVA_HOME does not contain bin/java.exe: $env:JAVA_HOME"
        }
    } else {
        $javaCommand = (Get-Command java -CommandType Application -ErrorAction Stop).Source
    }

    # Java writes -version to stderr; PS 5.1 turns redirected stderr into errors.
    $ErrorActionPreference = 'Continue'
    $versionOutput = & $javaCommand -version 2>&1
    $versionExitCode = $LASTEXITCODE
    $ErrorActionPreference = 'Stop'
    if ($versionExitCode -ne 0) {
        throw 'The selected Java could not report its version.'
    }
    if (($versionOutput -join "`n") -notmatch 'version\s+"25[.\"+\-]') {
        throw "JDK 25 is required (including for preview features). Selected Java: $javaCommand`n$versionOutput"
    }

    $javaOptions = @('--enable-preview', '--source', '25',
        '-Dstdout.encoding=UTF-8', '-Dstderr.encoding=UTF-8')
    $javaArguments = $javaOptions + $args
    if ($PSVersionTable.PSVersion.Major -le 5 -or $PSNativeCommandArgumentPassing -eq 'Legacy') {
        # Legacy splatting can split a -Dkey=value containing spaces. Quote each
        # argument using Windows argv rules, including quotes and trailing slashes.
        $javaArguments = @($javaArguments | ForEach-Object {
            $escaped = [regex]::Replace([string]$_, '(\\*)"', '$1$1\"')
            $escaped = [regex]::Replace($escaped, '(\\+)$', '$1$1')
            '"' + $escaped + '"'
        })
    }
    $savedOutputEncoding = [Console]::OutputEncoding
    try {
        [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
        & $javaCommand @javaArguments
        $javaExitCode = $LASTEXITCODE
    } finally {
        [Console]::OutputEncoding = $savedOutputEncoding
    }
    exit $javaExitCode
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    [Console]::Error.WriteLine('Install JDK 25 and set JAVA_HOME or PATH to its installation.')
    exit 1
}
