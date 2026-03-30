# Connect Claude Code CLI to an airun proxy (Windows).
# Usage:
#   .\connect-proxy.ps1 <proxy-url> <api-key>
#   irm https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.ps1 | iex
#   (when piped via iex, set $env:PROXY_URL and $env:PROXY_KEY first)

param(
    [string]$ProxyUrl = $env:PROXY_URL,
    [string]$ApiKey   = $env:PROXY_KEY
)

$ErrorActionPreference = 'Stop'

# ── Interactive input if args missing ──
if (-not $ProxyUrl) {
    $ProxyUrl = Read-Host '  Proxy URL (e.g. http://server:8080)'
}
if (-not $ApiKey) {
    $ApiKey = Read-Host '  API key (sk-ai-...)'
}

if (-not $ProxyUrl -or -not $ApiKey) {
    Write-Error 'Usage: .\connect-proxy.ps1 <proxy-url> <api-key>'
    exit 1
}

$ProxyUrl = $ProxyUrl.TrimEnd('/')

# ── Validate connection ──
Write-Host "`n  Connecting to $ProxyUrl... " -NoNewline

try {
    $headers = @{ 'x-api-key' = $ApiKey }
    $response = Invoke-RestMethod -Uri "$ProxyUrl/v1/models" -Headers $headers -TimeoutSec 10
} catch {
    Write-Host 'FAILED'
    $status = $_.Exception.Response.StatusCode.value__
    if ($status -eq 401) {
        Write-Error '  Invalid API key (HTTP 401)'
    } else {
        Write-Error "  Could not connect: $_"
    }
    exit 1
}

$models = @($response.data | ForEach-Object { $_.id })
if ($models.Count -eq 0) {
    Write-Host 'FAILED'
    Write-Error '  No models available.'
    exit 1
}

Write-Host "OK ($($models.Count) models)`n"
foreach ($m in $models) {
    Write-Host "  [x] $m"
}

$defaultModel = $models[0]
if ($models.Count -gt 1) {
    $chosen = Read-Host "`n  Default model [$defaultModel]"
    if ($chosen) { $defaultModel = $chosen }
}

# ── Write to ~/.claude/settings.json ──
$settingsDir = Join-Path $env:USERPROFILE '.claude'
$settingsFile = Join-Path $settingsDir 'settings.json'

if (-not (Test-Path $settingsDir)) {
    New-Item -ItemType Directory -Path $settingsDir -Force | Out-Null
}

if (Test-Path $settingsFile) {
    $settings = Get-Content -Raw $settingsFile | ConvertFrom-Json
} else {
    $settings = [PSCustomObject]@{}
}

# Ensure env property exists
if (-not ($settings.PSObject.Properties.Name -contains 'env')) {
    $settings | Add-Member -NotePropertyName 'env' -NotePropertyValue ([PSCustomObject]@{})
}

$envVars = @{
    'ANTHROPIC_AUTH_TOKEN'          = $ApiKey
    'ANTHROPIC_BASE_URL'            = $ProxyUrl
    'ANTHROPIC_DEFAULT_SONNET_MODEL' = $defaultModel
    'ANTHROPIC_DEFAULT_OPUS_MODEL'   = $defaultModel
    'ANTHROPIC_DEFAULT_HAIKU_MODEL'  = $defaultModel
    'API_TIMEOUT_MS'                = '3000000'
}

foreach ($kv in $envVars.GetEnumerator()) {
    if ($settings.env.PSObject.Properties.Name -contains $kv.Key) {
        $settings.env.$($kv.Key) = $kv.Value
    } else {
        $settings.env | Add-Member -NotePropertyName $kv.Key -NotePropertyValue $kv.Value
    }
}

$settings | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 $settingsFile

Write-Host "`n  Claude Code configured:"
Write-Host "    URL:   $ProxyUrl"
Write-Host "    Model: $defaultModel"
Write-Host "    File:  $settingsFile"
Write-Host "`n  Run 'claude' to start using the proxy.`n"
