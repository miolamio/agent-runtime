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
    [string]$ApiKey   = $env:PROXY_KEY,
    [switch]$Disconnect
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

# Shared v1 journal and three-way undo, compatible with the Go and Bash clients.
function Has-Prop($obj, $name) { return $null -ne $obj -and $obj.PSObject.Properties.Name -contains $name }
function Copy-Json($obj) {
    $wrapper = [PSCustomObject]@{ value=$obj }
    $copy = ConvertTo-Json -InputObject $wrapper -Depth 100 -Compress | ConvertFrom-Json
    return ,$copy.value
}
function Equal-Json($a, $b) {
    if ($null -eq $a -or $null -eq $b) { return $null -eq $a -and $null -eq $b }
    if ($a -is [PSCustomObject] -and $b -is [PSCustomObject]) {
        $ak = @($a.PSObject.Properties.Name); $bk = @($b.PSObject.Properties.Name)
        if ($ak.Count -ne $bk.Count) { return $false }
        foreach ($k in $ak) { if (-not (Has-Prop $b $k) -or -not (Equal-Json $a.$k $b.$k)) { return $false } }
        return $true
    }
    if ($a -is [array] -and $b -is [array]) {
        if ($a.Count -ne $b.Count) { return $false }
        for ($i=0; $i -lt $a.Count; $i++) { if (-not (Equal-Json $a[$i] $b[$i])) { return $false } }
        return $true
    }
    if (($a -is [string]) -ne ($b -is [string]) -or ($a -is [bool]) -ne ($b -is [bool])) { return $false }
    return $a -ceq $b
}
function Contains-Json($items, $value) {
    foreach ($item in $items) { if (Equal-Json $item $value) { return $true } }
    return $false
}
function Undo-Settings($current, $before, $after) {
    $result = Copy-Json $current
    $keys = @(@($before.PSObject.Properties.Name) + @($after.PSObject.Properties.Name) | Where-Object { $null -ne $_ } | Sort-Object -Unique)
    foreach ($k in $keys) {
        $hadOld = Has-Prop $before $k; $hadWritten = Has-Prop $after $k; $exists = Has-Prop $result $k
        $old = $before.$k; $written = $after.$k; $value = $result.$k
        if ($hadOld -eq $hadWritten -and (Equal-Json $old $written)) { continue }
        if ($exists -eq $hadWritten -and (Equal-Json $value $written)) {
            if ($hadOld) { Set-JsonProp $result $k $old } else { $result.PSObject.Properties.Remove($k) }
        } elseif ($value -is [PSCustomObject] -and $written -is [PSCustomObject]) {
            $oldMap = if ($old -is [PSCustomObject]) { $old } else { [PSCustomObject]@{} }
            $restored = Undo-Settings $value $oldMap $written
            if (-not $hadOld -and @($restored.PSObject.Properties.Name).Count -eq 0) { $result.PSObject.Properties.Remove($k) }
            else { Set-JsonProp $result $k $restored }
        } elseif ($value -is [array] -and $written -is [array]) {
            $restored = @($value | Where-Object { -not (Contains-Json $written $_) -or (Contains-Json $old $_) })
            if (-not $hadOld -and $restored.Count -eq 0) { $result.PSObject.Properties.Remove($k) }
            else { Set-JsonProp $result $k $restored }
        }
    }
    return $result
}
function Read-Document($path) {
    if (Test-Path -LiteralPath $path) { $doc = Get-Content -Raw -LiteralPath $path | ConvertFrom-Json }
    else { $doc = [PSCustomObject]@{} }
    if ($doc -isnot [PSCustomObject]) { throw "$path must contain a JSON object" }
    if (Has-Prop $doc '_airunBackup') {
        $b = $doc._airunBackup
        if ($b.version -ne 1 -or $b.before -isnot [PSCustomObject] -or $b.after -isnot [PSCustomObject]) { throw 'Unsupported or damaged airun settings backup' }
    }
    return $doc
}
function Add-Journal($current, $desired, $created) {
    $before = Copy-Json $current
    if (Has-Prop $current '_airunBackup') {
        $created = $current._airunBackup.created
        $before.PSObject.Properties.Remove('_airunBackup'); $before.PSObject.Properties.Remove('_airunManaged')
        $before = Undo-Settings $before $current._airunBackup.before $current._airunBackup.after
    }
    $desired.PSObject.Properties.Remove('_airunBackup'); $desired.PSObject.Properties.Remove('_airunManaged')
    $oldFields = [PSCustomObject]@{}; $newFields = [PSCustomObject]@{}
    foreach ($k in @($desired.PSObject.Properties.Name)) {
        if (-not (Has-Prop $before $k) -or -not (Equal-Json $before.$k $desired.$k)) {
            Set-JsonProp $newFields $k (Copy-Json $desired.$k)
            if (Has-Prop $before $k) { Set-JsonProp $oldFields $k (Copy-Json $before.$k) }
        }
    }
    Set-JsonProp $desired '_airunManaged' $true
    Set-JsonProp $desired '_airunBackup' ([PSCustomObject]@{version=1;created=$created;before=$oldFields;after=$newFields})
    return $desired
}
function Write-Document($path, $doc) {
    $dir = Split-Path $path
    if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    $tmp = Join-Path $dir ([IO.Path]::GetRandomFileName())
    try {
        [IO.File]::WriteAllText($tmp, '', (New-Object Text.UTF8Encoding($false)))
        if ($env:OS -ne 'Windows_NT') { & chmod 600 $tmp; if ($LASTEXITCODE -ne 0) { throw 'Cannot secure settings file' } }
        [IO.File]::WriteAllText($tmp, ($doc | ConvertTo-Json -Depth 100) + "`n", (New-Object Text.UTF8Encoding($false)))
        Move-Item -LiteralPath $tmp -Destination $path -Force
    } finally { if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp } }
}
if ($Disconnect -or ($args -contains '--disconnect') -or $ProxyUrl -eq '--disconnect' -or $ProxyUrl -eq 'disconnect') {
    foreach ($path in @($SettingsFile,$ClaudeJSON)) {
        $doc = Read-Document $path
        if (-not (Has-Prop $doc '_airunBackup')) { continue }
        $backup = $doc._airunBackup
        $doc.PSObject.Properties.Remove('_airunBackup'); $doc.PSObject.Properties.Remove('_airunManaged')
        $restored = Undo-Settings $doc $backup.before $backup.after
        if ($backup.created -and @($restored.PSObject.Properties.Name).Count -eq 0) { Remove-Item -LiteralPath $path }
        else { Write-Document $path $restored }
    }
    Write-Host '  Previous settings restored; subsequent user edits preserved.'
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

# Prefer glm-5.3 if available
$defaultModel = $models[0]
if ($models -contains 'glm-5.3') { $defaultModel = 'glm-5.3' }
if ($models.Count -gt 1) {
    $chosen = Read-Host "`n  Default model [$defaultModel]"
    if ($chosen) { $defaultModel = $chosen }
}

# Parse both files before any mutation; retain approvals and unrelated settings.
$currentSettings = Read-Document $SettingsFile
$currentClaude = Read-Document $ClaudeJSON
$settings = Copy-Json $currentSettings
$cj = Copy-Json $currentClaude
if ((Has-Prop $settings 'env') -and $settings.env -isnot [PSCustomObject]) { throw 'env must be an object' }
if (-not (Has-Prop $settings 'env')) { Set-JsonProp $settings 'env' ([PSCustomObject]@{}) }
$envVars = @{
    'ANTHROPIC_AUTH_TOKEN'=$ApiKey; 'ANTHROPIC_BASE_URL'=$ProxyUrl
    'ANTHROPIC_DEFAULT_SONNET_MODEL'=$defaultModel; 'ANTHROPIC_DEFAULT_OPUS_MODEL'=$defaultModel
    'ANTHROPIC_DEFAULT_HAIKU_MODEL'=$defaultModel; 'API_TIMEOUT_MS'='3000000'
}
foreach ($kv in $envVars.GetEnumerator()) { Set-JsonProp $settings.env $kv.Key $kv.Value }
try { $ver = (claude --version 2>$null | Select-Object -First 1) -replace '[^0-9.]','' } catch { $ver = '99.0.0' }
if (-not $ver) { $ver = '99.0.0' }
$keyTail = if ($ApiKey.Length -gt 20) { $ApiKey.Substring($ApiKey.Length - 20) } else { $ApiKey }
Set-JsonProp $cj 'hasCompletedOnboarding' $true
Set-JsonProp $cj 'hasTrustDialogAccepted' $true
Set-JsonProp $cj 'lastOnboardingVersion' $ver
Set-JsonProp $cj 'autoUpdaterStatus' 'disabled'
if (-not (Has-Prop $cj 'numStartups')) { Set-JsonProp $cj 'numStartups' 184 }
if (-not (Has-Prop $cj 'userID')) { Set-JsonProp $cj 'userID' ([Guid]::NewGuid().ToString('N')) }
if (-not (Has-Prop $cj 'projects')) { Set-JsonProp $cj 'projects' ([PSCustomObject]@{}) }
if ((Has-Prop $cj 'customApiKeyResponses') -and $cj.customApiKeyResponses -isnot [PSCustomObject]) { throw 'customApiKeyResponses must be an object' }
if (-not (Has-Prop $cj 'customApiKeyResponses')) { Set-JsonProp $cj 'customApiKeyResponses' ([PSCustomObject]@{}) }
$car = $cj.customApiKeyResponses
$approved = @()
if (Has-Prop $car 'approved') { $approved = @($car.approved) }
if (-not (Contains-Json $approved $keyTail)) { $approved += $keyTail }
Set-JsonProp $car 'approved' $approved
if (-not (Has-Prop $car 'rejected')) { Set-JsonProp $car 'rejected' @() }
$settings = Add-Journal $currentSettings $settings (-not (Test-Path -LiteralPath $SettingsFile))
$cj = Add-Journal $currentClaude $cj (-not (Test-Path -LiteralPath $ClaudeJSON))
Write-Document $SettingsFile $settings
Write-Document $ClaudeJSON $cj
Write-Host "`n  Claude Code configured:"
Write-Host "    URL:      $ProxyUrl"
Write-Host "    Model:    $defaultModel"
Write-Host "    Settings: $SettingsFile"
Write-Host "    Auth:     $ClaudeJSON (onboarding bypassed)"
Write-Host "`n  Run 'claude' to start using the proxy."
Write-Host "  To disconnect: .\connect-proxy.ps1 --disconnect`n"
