$ErrorActionPreference = "Stop"

$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "CLIProxyAPI-Lite" }
$Binary = Join-Path $InstallDir "cliproxy-lite.exe"
$TaskName = "CLIProxyAPI Lite"

if (!(Test-Path $Binary)) {
    throw "Binary not found: $Binary. Run scripts/install.ps1 first."
}

$Action = New-ScheduledTaskAction -Execute $Binary -Argument "serve --no-open" -WorkingDirectory $InstallDir
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$Principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited
$Settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Days 3650) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)

Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings -Force | Out-Null
Write-Host "Installed logon task: $TaskName"
Start-ScheduledTask -TaskName $TaskName
