# Installs the latest release of this gentle-ai fork on Windows:
#
#   irm https://raw.githubusercontent.com/santiago-ramos-02/gentle-ai/main/scripts/t3-install.ps1 | iex
#
# It replaces the gentle-ai already on PATH, or installs to
# %LOCALAPPDATA%\Programs\gentle-ai and adds that to the user PATH. From then on
# gentle-ai updates itself from the fork's releases. GENTLE_AI_FORK names
# another fork to install from.

& {
  $ErrorActionPreference = 'Stop'
  $repo = if ($env:GENTLE_AI_FORK) { $env:GENTLE_AI_FORK } else { 'santiago-ramos-02/gentle-ai' }
  $arch = if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }

  $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
  $version = $release.tag_name -replace '^v', ''
  $name = "gentle-ai_${version}_windows_$arch"
  $download = "https://github.com/$repo/releases/download/$($release.tag_name)"

  $work = Join-Path ([IO.Path]::GetTempPath()) "gentle-ai-install-$([Guid]::NewGuid())"
  New-Item -ItemType Directory $work | Out-Null
  try {
    $archive = Join-Path $work "$name.zip"
    Invoke-WebRequest "$download/$name.zip" -OutFile $archive -UseBasicParsing
    $checksums = (Invoke-WebRequest "$download/checksums.txt" -UseBasicParsing).Content
    $expected = ($checksums -split "`n" | Where-Object { $_ -match "^([0-9a-f]{64})\s+$([regex]::Escape("$name.zip"))\s*$" } |
      ForEach-Object { $Matches[1] }) | Select-Object -First 1
    if (-not $expected) { throw "$name.zip is not listed in the release's checksums.txt" }
    if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $expected) { throw "$name.zip does not match its checksum" }
    Expand-Archive $archive -DestinationPath $work
    $binary = Join-Path $work "$name\gentle-ai.exe"

    $existing = Get-Command gentle-ai -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $target = if ($existing) { $existing.Source } else { Join-Path $env:LOCALAPPDATA 'Programs\gentle-ai\gentle-ai.exe' }
    New-Item -ItemType Directory -Force (Split-Path $target) | Out-Null
    # A running gentle-ai cannot be overwritten, but it can be moved aside.
    if (Test-Path $target) {
      Remove-Item "$target.old" -Force -ErrorAction SilentlyContinue
      Move-Item $target "$target.old" -Force
    }
    Move-Item $binary $target
    Remove-Item "$target.old" -Force -ErrorAction SilentlyContinue

    if (-not $existing) {
      $dir = Split-Path $target
      $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
      if (($userPath -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', ($(if ($userPath) { "$userPath;" }) + $dir), 'User')
        Write-Host "Added $dir to your PATH; open a new terminal to use gentle-ai."
      }
      $env:Path = "$env:Path;$dir"
    }
    Write-Host "Installed $(& $target version) at $target"
  } finally {
    Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
  }
}
