$ErrorActionPreference = "Stop"

function Wait-HttpOk {
  param(
    [Parameter(Mandatory=$true)][string]$Url,
    [int]$TimeoutSeconds = 30
  )
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  while ((Get-Date) -lt $deadline) {
    try {
      $resp = Invoke-WebRequest -UseBasicParsing -Method Get -Uri $Url -TimeoutSec 2
      if ($resp.StatusCode -eq 200) { return }
    } catch {
      Start-Sleep -Milliseconds 300
    }
  }
  throw "Timed out waiting for $Url"
}

Write-Host "Starting cluster via docker compose..."
docker compose up -d | Out-Null

Wait-HttpOk -Url "http://localhost:8080/healthz"
Wait-HttpOk -Url "http://localhost:8081/healthz"
Wait-HttpOk -Url "http://localhost:8082/healthz"

$key = "foo"
$valuePlain = "bar"
$valueB64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($valuePlain))

$bodyObj = @{
  value  = $valueB64
  ttl_ms = 60000
}
$body = $bodyObj | ConvertTo-Json -Compress

Write-Host "SET $key via node2 (8081)"
Invoke-WebRequest -UseBasicParsing -Method Post -Uri "http://localhost:8081/$key" -ContentType "application/json" -Body $body | Out-Null

Write-Host "GET $key via node1 (8080)"
$r1 = Invoke-WebRequest -UseBasicParsing -Method Get -Uri "http://localhost:8080/$key"
Write-Host $r1.Content

Write-Host "GET $key via node3 (8082)"
$r3 = Invoke-WebRequest -UseBasicParsing -Method Get -Uri "http://localhost:8082/$key"
Write-Host $r3.Content

Write-Host "Smoke test OK"
