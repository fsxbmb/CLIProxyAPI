$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = if (Test-Path (Join-Path $ScriptDir "cmd")) { $ScriptDir } else { Split-Path -Parent $ScriptDir }
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "CLIProxyAPI-Lite" }
$Target = Join-Path $InstallDir "cliproxy-lite.exe"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

# Release archives contain the executable at the package root. Source trees
# may contain bin\cliproxy-lite.exe; otherwise Go is used to build it.
$Bundled = Join-Path $ProjectDir "cliproxy-lite.exe"
$SourceBinary = Join-Path $ProjectDir "bin\cliproxy-lite.exe"
if (Test-Path $Bundled) {
    Copy-Item $Bundled $Target -Force
} elseif (Test-Path $SourceBinary) {
    Copy-Item $SourceBinary $Target -Force
} elseif (Get-Command go -ErrorAction SilentlyContinue) {
    Push-Location $ProjectDir
    try {
        $Version = if ($env:VERSION) { $env:VERSION } else { "dev" }
        $Commit = if ($env:COMMIT) { $env:COMMIT } else { "none" }
        $BuildDate = if ($env:BUILD_DATE) { $env:BUILD_DATE } else { (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ") }
        $Ldflags = "-s -w -X main.version=$Version -X main.commit=$Commit -X main.buildDate=$BuildDate"
        $env:CGO_ENABLED = "0"
        go build -trimpath -ldflags $Ldflags -o $Target ./cmd/cliproxy-lite
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
        Pop-Location
    }
} else {
    throw "No bundled cliproxy-lite.exe found and Go is not installed. Install Go from https://go.dev/dl/ or use a release archive."
}

Write-Host "Installed CLIProxyAPI Lite to $Target"
Write-Host "Initialize: $Target init"
Write-Host "Run:        $Target serve"
Write-Host "API:        http://127.0.0.1:8317/v1"
Write-Host "Web UI:     http://127.0.0.1:8318/ui/"
