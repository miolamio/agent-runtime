# Connect/disconnect Claude Code CLI to an airun proxy (Windows).
#
# Connect:
#   .\connect-proxy.ps1 <proxy-url> <api-key>
#   $env:PROXY_URL='http://server:8080'; $env:PROXY_KEY='sk-ai-token'
#   irm https://raw.githubusercontent.com/miolamio/agent-runtime/main/scripts/connect-proxy.ps1 | iex
#
# Disconnect:
#   .\connect-proxy.ps1 --disconnect

param(
    [string]$ProxyUrl = $env:PROXY_URL,
    [string]$ApiKey   = $env:PROXY_KEY
)

$ErrorActionPreference = 'Stop'
$SettingsDir  = Join-Path $env:USERPROFILE '.claude'
$SettingsFile = Join-Path $SettingsDir 'settings.json'
$ClaudeJSON   = Join-Path $env:USERPROFILE '.claude.json'

# ── Helper: merge a property into a PSObject ──
function Set-JsonProp($obj, $name, $value) {
    if ($obj.PSObject.Properties.Name -contains $name) {
        $obj.$name = $value
    } else {
        $obj | Add-Member -NotePropertyName $name -NotePropertyValue $value
    }
}

# ── Disconnect mode ──
if ($ProxyUrl -eq '--disconnect' -or $ProxyUrl -eq 'disconnect') {
    $removed = 0
    $envKeys = @('ANTHROPIC_AUTH_TOKEN','ANTHROPIC_BASE_URL','ANTHROPIC_DEFAULT_SONNET_MODEL',
                 'ANTHROPIC_DEFAULT_OPUS_MODEL','ANTHROPIC_DEFAULT_HAIKU_MODEL','API_TIMEOUT_MS')

    # Clean settings.json
    if (Test-Path $SettingsFile) {
        $settings = Get-Content -Raw $SettingsFile | ConvertFrom-Json
        if ($settings.PSObject.Properties.Name -contains 'env') {
            foreach ($k in $envKeys) {
                if ($settings.env.PSObject.Properties.Name -contains $k) {
                    $settings.env.PSObject.Properties.Remove($k)
                    $removed++
                }
            }
            $settings | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 $SettingsFile
        }
    }

    # Clean claude.json
    if (Test-Path $ClaudeJSON) {
        $cj = Get-Content -Raw $ClaudeJSON | ConvertFrom-Json
        $managed = $false
        if ($cj.PSObject.Properties.Name -contains '_airunManaged') {
            $managed = $cj._airunManaged -eq $true
        }
        if ($managed) {
            Remove-Item $ClaudeJSON -Force
            $removed++
        } elseif ($cj.PSObject.Properties.Name -contains 'customApiKeyResponses') {
            $cj.PSObject.Properties.Remove('customApiKeyResponses')
            if ($cj.PSObject.Properties.Name -contains '_airunManaged') {
                $cj.PSObject.Properties.Remove('_airunManaged')
            }
            $cj | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 $ClaudeJSON
            $removed++
        }
    }

    if ($removed -eq 0) {
        Write-Host '  No proxy settings found.'
    } else {
        Write-Host '  Proxy settings removed.'
        Write-Host '  Claude Code will use its default Anthropic API.'
    }
    exit
}

# ── Connect mode ──
if (-not $ProxyUrl) { $ProxyUrl = Read-Host '  Proxy URL (e.g. http://server:8080)' }
if (-not $ApiKey)   { $ApiKey   = Read-Host '  API key (sk-ai-...)' }

if (-not $ProxyUrl -or -not $ApiKey) {
    Write-Error 'Usage: .\connect-proxy.ps1 <proxy-url> <api-key>'
    Write-Error '       .\connect-proxy.ps1 --disconnect'
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
    if ($status -eq 401) { Write-Error '  Invalid API key (HTTP 401)' }
    else { Write-Error "  Could not connect: $_" }
    exit 1
}

$models = @($response.data | ForEach-Object { $_.id })
if ($models.Count -eq 0) {
    Write-Host 'FAILED'; Write-Error '  No models available.'; exit 1
}

Write-Host "OK ($($models.Count) models)`n"
foreach ($m in $models) { Write-Host "  [x] $m" }

# Prefer glm-5.1 if available
$defaultModel = $models[0]
if ($models -contains 'glm-5.1') { $defaultModel = 'glm-5.1' }
if ($models.Count -gt 1) {
    $chosen = Read-Host "`n  Default model [$defaultModel]"
    if ($chosen) { $defaultModel = $chosen }
}

# ── 1. Write ~/.claude/settings.json ──
if (-not (Test-Path $SettingsDir)) { New-Item -ItemType Directory -Path $SettingsDir -Force | Out-Null }

if (Test-Path $SettingsFile) {
    $settings = Get-Content -Raw $SettingsFile | ConvertFrom-Json
} else {
    $settings = [PSCustomObject]@{}
}

if (-not ($settings.PSObject.Properties.Name -contains 'env')) {
    $settings | Add-Member -NotePropertyName 'env' -NotePropertyValue ([PSCustomObject]@{})
}

$envVars = @{
    'ANTHROPIC_AUTH_TOKEN'           = $ApiKey
    'ANTHROPIC_BASE_URL'             = $ProxyUrl
    'ANTHROPIC_DEFAULT_SONNET_MODEL' = $defaultModel
    'ANTHROPIC_DEFAULT_OPUS_MODEL'   = $defaultModel
    'ANTHROPIC_DEFAULT_HAIKU_MODEL'  = $defaultModel
    'API_TIMEOUT_MS'                 = '3000000'
}
foreach ($kv in $envVars.GetEnumerator()) { Set-JsonProp $settings.env $kv.Key $kv.Value }

$settings | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 $SettingsFile

# ── 2. Write ~/.claude.json (onboarding/auth bypass) ──
# Detect Claude Code version
try { $ver = (claude --version 2>$null | Select-Object -First 1) -replace '[^0-9.]','' } catch { $ver = '99.0.0' }
if (-not $ver) { $ver = '99.0.0' }

# Last 20 chars of API key for trust
$keyTail = if ($ApiKey.Length -gt 20) { $ApiKey.Substring($ApiKey.Length - 20) } else { $ApiKey }

if (Test-Path $ClaudeJSON) {
    $cj = Get-Content -Raw $ClaudeJSON | ConvertFrom-Json
} else {
    $uid = -join ((1..64) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) })
    $cj = [PSCustomObject]@{ userID = $uid; numStartups = 184; projects = [PSCustomObject]@{} }
}

Set-JsonProp $cj 'hasCompletedOnboarding' $true
Set-JsonProp $cj 'hasTrustDialogAccepted' $true
Set-JsonProp $cj 'lastOnboardingVersion'  $ver
Set-JsonProp $cj 'autoUpdaterStatus'      'disabled'
Set-JsonProp $cj '_airunManaged'          $true

# API key trust
$car = [PSCustomObject]@{ approved = @($keyTail); rejected = @() }
Set-JsonProp $cj 'customApiKeyResponses' $car

$cj | ConvertTo-Json -Depth 10 | Set-Content -Encoding UTF8 $ClaudeJSON

Write-Host "`n  Claude Code configured:"
Write-Host "    URL:      $ProxyUrl"
Write-Host "    Model:    $defaultModel"
Write-Host "    Settings: $SettingsFile"
Write-Host "    Auth:     $ClaudeJSON (onboarding bypassed)"
Write-Host "`n  Run 'claude' to start using the proxy."
Write-Host "  To disconnect: .\connect-proxy.ps1 --disconnect`n"
