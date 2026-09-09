param(
    [string]$DbHost = "67.230.183.84",
    [string]$SshUser = "root",
    [string]$KeyPath = "C:\Users\Administrator\.ssh\starnexus_ops_ed25519",
    [int]$ChannelId = 69,
    [int]$DurationSeconds = 600,
    [int]$PollSeconds = 5,
    [int]$LookbackMinutes = 3,
    [string]$OutputPath = (Join-Path $env:TEMP "starnexus-sub2api-latency.csv")
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $KeyPath)) {
    throw "SSH key not found: $KeyPath"
}

$columns = @(
    "captured_at", "star_id", "sub_id", "completion_skew_ms",
    "user_id", "account_id", "model", "prompt_tokens", "output_tokens",
    "star_total_ms", "sub_forward_ms", "total_delta_ms",
    "star_frt_ms", "sub_ttft_ms", "star_local_overhead_ms", "omitted_before_final_forward_ms",
    "failover_count", "failover_accounts", "failover_proxies", "failover_statuses",
    "star_request_id", "sub_request_id"
)

if (-not (Test-Path -LiteralPath $OutputPath)) {
    [pscustomobject]([ordered]@{}) | Select-Object $columns |
        Export-Csv -LiteralPath $OutputPath -NoTypeInformation -Encoding UTF8
}

function Invoke-DbQuery {
    param(
        [Parameter(Mandatory)][string]$Database,
        [Parameter(Mandatory)][string]$Sql
    )

    $escapedSql = $Sql.Replace('"', '\"')
    $remote = "docker exec db-postgres psql -U postgres -d $Database -At -F '|' -c `"$escapedSql`""
    $result = & ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10 `
        -i $KeyPath "$SshUser@$DbHost" $remote 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Query failed for ${Database}: $($result -join [Environment]::NewLine)"
    }
    return @($result | Where-Object { $_ -and -not $_.StartsWith("NOTICE:") })
}

function Convert-StarRows {
    param([string[]]$Lines)

    foreach ($line in $Lines) {
        $p = $line -split '\|', 9
        if ($p.Count -ne 9) { continue }
        [pscustomobject]@{
            Id          = [long]$p[0]
            EndMs       = [long]$p[1]
            UserId      = [long]$p[2]
            Model       = $p[3]
            Prompt      = [long]$p[4]
            Output      = [long]$p[5]
            TotalMs     = [long]$p[6]
            FrtMs       = [long]$p[7]
            RequestId   = $p[8]
        }
    }
}

function Convert-SubRows {
    param([string[]]$Lines)

    foreach ($line in $Lines) {
        $p = $line -split '\|', 9
        if ($p.Count -ne 9) { continue }
        [pscustomobject]@{
            Id          = [long]$p[0]
            EndMs       = [long]$p[1]
            AccountId   = [long]$p[2]
            Model       = $p[3]
            Prompt      = [long]$p[4]
            Output      = [long]$p[5]
            ForwardMs   = [long]$p[6]
            TtftMs      = if ($p[7] -eq "") { $null } else { [long]$p[7] }
            RequestId   = $p[8]
        }
    }
}

function Convert-FailoverRows {
    param([string[]]$Lines)

    foreach ($line in $Lines) {
        $p = $line -split '\|', 7
        if ($p.Count -ne 7) { continue }
        [pscustomobject]@{
            ClientRequestId = $p[0]
            AccountId      = $p[1]
            ProxyHost      = $p[2]
            ProxyPort      = $p[3]
            Status         = $p[4]
            SwitchCount    = $p[5]
            AtMs           = [long]$p[6]
        }
    }
}

$emitted = [System.Collections.Generic.HashSet[long]]::new()
$deadline = (Get-Date).AddSeconds($DurationSeconds)

Write-Host "Collecting latency correlations into $OutputPath"

while ((Get-Date) -lt $deadline) {
    try {
        $starSql = @"
select id,
       created_at::bigint * 1000,
       user_id,
       model_name,
       coalesce(nullif(other::jsonb->>'raw_prompt_tokens','')::bigint, prompt_tokens::bigint),
       coalesce(nullif(other::jsonb->>'raw_completion_tokens','')::bigint, completion_tokens::bigint),
       use_time_ms::bigint,
       coalesce(nullif(other::jsonb->>'frt','')::bigint, 0),
       request_id
from logs
where channel_id = $ChannelId
  and created_at >= extract(epoch from now() - interval '$LookbackMinutes minutes')::bigint
  and coalesce(other::jsonb->>'request_path','') = '/v1/responses'
order by id desc
limit 500
"@

        $subSql = @"
select id,
       floor(extract(epoch from created_at) * 1000)::bigint,
       account_id,
       model,
       (coalesce(input_tokens,0) + coalesce(cache_read_tokens,0) + coalesce(cache_creation_tokens,0))::bigint,
       coalesce(output_tokens,0)::bigint,
       duration_ms::bigint,
       coalesce(first_token_ms::text,''),
       request_id
from usage_logs
where created_at >= now() - interval '$LookbackMinutes minutes'
  and inbound_endpoint = '/v1/responses'
order by id desc
limit 500
"@

        $failoverSql = @"
select coalesce(client_request_id,''),
       coalesce(account_id::text, extra::jsonb->>'account_id', ''),
       coalesce(extra::jsonb->>'proxy_host',''),
       coalesce(extra::jsonb->>'proxy_port',''),
       coalesce(extra::jsonb->>'upstream_status',''),
       coalesce(extra::jsonb->>'switch_count',''),
       floor(extract(epoch from created_at) * 1000)::bigint
from ops_system_logs
where created_at >= now() - interval '$LookbackMinutes minutes'
  and message = 'openai.upstream_failover_switching'
order by id desc
limit 500
"@

        $starRows = @(Convert-StarRows (Invoke-DbQuery -Database "starnex" -Sql $starSql))
        $subRows = @(Convert-SubRows (Invoke-DbQuery -Database "sub2api" -Sql $subSql))
        $failoverRows = @(Convert-FailoverRows (Invoke-DbQuery -Database "sub2api" -Sql $failoverSql))
        $usedSubIds = [System.Collections.Generic.HashSet[long]]::new()

        foreach ($star in ($starRows | Sort-Object EndMs)) {
            if ($emitted.Contains($star.Id)) { continue }

            $matches = @($subRows | Where-Object {
                -not $usedSubIds.Contains($_.Id) -and
                $_.Model -eq $star.Model -and
                $_.Prompt -eq $star.Prompt -and
                $_.Output -eq $star.Output -and
                [math]::Abs($_.EndMs - $star.EndMs) -le 1500
            } | Sort-Object { [math]::Abs($_.EndMs - $star.EndMs) })

            if ($matches.Count -eq 0) { continue }
            $sub = $matches[0]
            [void]$usedSubIds.Add($sub.Id)
            [void]$emitted.Add($star.Id)

            $deltaMs = $star.TotalMs - $sub.ForwardMs
            $starLocalOverheadMs = $null
            $omittedBeforeFinalForwardMs = $null
            if ($star.FrtMs -gt 0 -and $null -ne $sub.TtftMs) {
                # Both FRT metrics terminate on the same semantic first-output event.
                # The difference is time omitted by the final successful Forward result,
                # including any earlier failed Forward attempt and failover handling.
                $omittedBeforeFinalForwardMs = $star.FrtMs - $sub.TtftMs
                $starLocalOverheadMs = $deltaMs - $omittedBeforeFinalForwardMs
            }

            $clientRequestId = $sub.RequestId -replace '^client:', ''
            $failovers = @($failoverRows | Where-Object { $_.ClientRequestId -eq $clientRequestId } | Sort-Object AtMs)

            $row = [pscustomobject][ordered]@{
                captured_at              = (Get-Date).ToString("o")
                star_id                  = $star.Id
                sub_id                   = $sub.Id
                completion_skew_ms       = $sub.EndMs - $star.EndMs
                user_id                  = $star.UserId
                account_id               = $sub.AccountId
                model                    = $star.Model
                prompt_tokens            = $star.Prompt
                output_tokens            = $star.Output
                star_total_ms            = $star.TotalMs
                sub_forward_ms            = $sub.ForwardMs
                total_delta_ms            = $deltaMs
                star_frt_ms               = $star.FrtMs
                sub_ttft_ms               = $sub.TtftMs
                star_local_overhead_ms    = $starLocalOverheadMs
                omitted_before_final_forward_ms = $omittedBeforeFinalForwardMs
                failover_count            = $failovers.Count
                failover_accounts         = ($failovers.AccountId -join ";")
                failover_proxies          = (@($failovers | ForEach-Object { "$($_.ProxyHost):$($_.ProxyPort)" }) -join ";")
                failover_statuses         = ($failovers.Status -join ";")
                star_request_id           = $star.RequestId
                sub_request_id            = $sub.RequestId
            }
            $row | Export-Csv -LiteralPath $OutputPath -NoTypeInformation -Append -Encoding UTF8
            $row | Format-Table star_id, sub_id, model, star_total_ms, sub_forward_ms, total_delta_ms, omitted_before_final_forward_ms, failover_count, failover_accounts -AutoSize
        }
    }
    catch {
        Write-Warning $_
    }

    Start-Sleep -Seconds $PollSeconds
}

Write-Host "Collection complete: $OutputPath"
