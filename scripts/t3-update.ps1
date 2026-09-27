# Keeps the installed gentle-ai on upstream main plus this fork's headless API.
#
# Rebases the t3/headless-api branch onto upstream/main, runs the API tests,
# and installs the build over the gentle-ai on PATH. Any failure (dirty tree,
# rebase conflict, failing tests, a build that cannot answer `api describe`)
# stops before the install, so the working binary is never replaced by a
# broken one. Runs daily from the "gentle-ai T3 update" scheduled task.
#
# The version is upstream's latest release plus the upstream commit built, such
# as 3.7.0-t3.d23f490, so hosts see the real release. Self-update must stay
# off (GENTLE_AI_NO_SELF_UPDATE=1): it would replace this build with a stock one.

$ErrorActionPreference = 'Stop'
$Branch = 't3/headless-api'
$Repo = Split-Path -Parent $PSScriptRoot
$Target = (Get-Command gentle-ai -CommandType Application -ErrorAction SilentlyContinue |
  Select-Object -First 1).Source
if (-not $Target) { $Target = Join-Path $HOME 'go\bin\gentle-ai.exe' }
$Log = Join-Path $HOME '.gentle-ai\t3-update.log'
New-Item -ItemType Directory -Force (Split-Path $Log) | Out-Null

function Write-Log([string]$Message) {
  $line = "$(Get-Date -Format s) $Message"
  Write-Host $line
  Add-Content -Path $Log -Value $line
}

function Invoke-Git {
  $output = & git -C $Repo @args 2>&1
  if ($LASTEXITCODE -ne 0) { throw "git $($args -join ' ') failed: $output" }
  $output
}

try {
  if ((Invoke-Git rev-parse --abbrev-ref HEAD) -ne $Branch) { throw "$Repo is not on $Branch" }
  if (Invoke-Git status --porcelain) { throw "$Repo has uncommitted changes" }

  Invoke-Git fetch --quiet upstream --tags | Out-Null
  $upstream = Invoke-Git rev-parse upstream/main
  & git -C $Repo merge-base --is-ancestor $upstream HEAD
  if ($LASTEXITCODE -ne 0) {
    & git -C $Repo rebase --quiet upstream/main 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
      & git -C $Repo rebase --abort 2>&1 | Out-Null
      throw "rebase onto upstream/main ($($upstream.Substring(0, 9))) conflicts; resolve it by hand"
    }
  }

  # Releases are tagged off main, so `git describe` would find an ancient tag.
  $release = Invoke-Git tag --sort=-v:refname | Where-Object { $_ -match '^v\d+\.\d+\.\d+$' } | Select-Object -First 1
  $version = "$($release -replace '^v', '')-t3.$($upstream.Substring(0, 7))"
  $installed = if (Test-Path $Target) { (& $Target version 2>$null) -replace '^gentle-ai ', '' }
  if ($installed -eq $version) {
    Write-Log "up to date at $version"
    exit 0
  }

  Push-Location $Repo
  try {
    & go test ./internal/api/... ./internal/service/... 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'API tests failed; run go test ./internal/api/... ./internal/service/...' }
    $build = Join-Path ([IO.Path]::GetTempPath()) "gentle-ai-$version.exe"
    & go build -buildvcs=false -ldflags "-X main.version=$version" -o $build ./cmd/gentle-ai
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
  } finally {
    Pop-Location
  }

  $describe = '{}' | & $build api describe | Select-Object -Last 1 | ConvertFrom-Json
  if ($describe.type -ne 'result' -or $describe.data.version -ne $version) { throw 'the new build does not answer api describe' }

  # A running gentle-ai.exe cannot be overwritten on Windows, but it can be renamed.
  $previous = "$Target.previous"
  if (Test-Path $previous) { Remove-Item $previous -Force -ErrorAction SilentlyContinue }
  if (Test-Path $Target) { Move-Item $Target $previous -Force }
  Move-Item $build $Target -Force
  Write-Log "installed $version (was $installed)"
} catch {
  Write-Log "FAILED: $_"
  exit 1
}
