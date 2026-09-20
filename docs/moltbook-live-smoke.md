# Moltbook identity smoke

Tracks [MOLT-003 / #25](https://github.com/safal207/Liminal-Rail-Metro/issues/25).
The identity semantics baseline is [PR #27](https://github.com/safal207/Liminal-Rail-Metro/pull/27),
merged at `08cfd178cc166ab9601f8a091c6c08e43391e2be`.

`cmd/moltbook-smoke` exercises a single bounded path:

```
identity verification -> Station -> explicit local Metro authority
  -> VERIFY_EVIDENCE of a local control receipt -> bound Metro receipt
```

The evidence backend runs the existing `metro.Verify` over a deterministic
control fixture. It does not contact an external AgentProof service. The Station's
allowlisted target remains `agentproof`; the report explicitly identifies the
actual backend as `metro.Verify/local-control-fixture`. Authority uses the
existing DecisionPlane gate and a separate local policy restricted to this
read-only fixture. A verified identity alone is insufficient for dispatch.

## Offline rehearsal

From the repository root, with the Go version specified by `go.mod`:

```sh
go run ./cmd/moltbook-smoke > /tmp/moltbook-rehearsal.json
go run ./cmd/moltbook-smoke -verify-report /tmp/moltbook-rehearsal.json
```

The default mode never reads credentials or uses a network transport. It supplies
a synthetic identity response to the real adapter, then exercises the Station,
authority, fixture verification and receipt checks. Its report must say:

```json
{
  "mode": "offline_rehearsal",
  "status": "PASS",
  "live_identity_verified": false,
  "identity_attempts": 1,
  "authority_calls": 1,
  "evidence_calls": 1,
  "evidence_backend": "metro.Verify/local-control-fixture"
}
```

This is an excerpt; the full JSON also contains revision metadata, timestamps,
the bound Station result and the receipt hash. CI performs this rehearsal and
saved-report verification without live credentials.

## Live prerequisites

1. Developer access has been approved and an application key is available.
2. The operator has a fresh, short-lived identity token for a test agent.
3. The binary is built from a clean, reviewed Git revision with VCS metadata.

The live runner checks the recorded revision before reading credentials. Build
outside the checkout so generated files do not make later builds dirty:

```sh
go build -buildvcs=true -o /tmp/moltbook-smoke ./cmd/moltbook-smoke
```

The only secret inputs are `MOLTBOOK_APP_KEY` and `MOLTBOOK_IDENTITY_TOKEN` in the
process environment. Do not put their values in command arguments, shell history,
Git, CI, screenshots, issues or chat. The runner does not obtain tokens or keys.

With credentials injected into the process environment, run **once**:

```sh
/tmp/moltbook-smoke -live > /tmp/moltbook-live.json
/tmp/moltbook-smoke -verify-report /tmp/moltbook-live.json
```

The adapter uses the fixed HTTPS Moltbook verification URL, refuses redirects,
enforces its timeout and response-size limit, and performs no automatic retry.
An invalid or unknown identity stops before authority and evidence dispatch.
Missing credentials or a dirty/unrecorded build return `BLOCKED` without sending
the identity request. Exit status is 0 for success/help, 1 for failure/blocking,
and 2 for invalid arguments. Check both the exit status and report status.

### Windows PowerShell

This example builds first, prompts for hidden credential input, restores the
previous environment in `finally`, and writes BOM-free UTF-8 JSON compatible with
the verifier. Run it in your own interactive terminal after access is granted.
Do not paste credential values into the script.

```powershell
$smokeExe = Join-Path $env:TEMP 'moltbook-smoke.exe'
$reportPath = Join-Path $env:TEMP ('moltbook-live-' + [guid]::NewGuid().ToString('N') + '.json')
go build -buildvcs=true -o $smokeExe ./cmd/moltbook-smoke
if ($LASTEXITCODE -ne 0) { throw 'Build failed' }

function Read-SmokeSecret([string]$Prompt) {
    $secure = Read-Host $Prompt -AsSecureString
    $buffer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { [Runtime.InteropServices.Marshal]::PtrToStringBSTR($buffer) }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($buffer)
        $secure.Dispose()
    }
}

$priorKey = $env:MOLTBOOK_APP_KEY
$priorToken = $env:MOLTBOOK_IDENTITY_TOKEN
try {
    $env:MOLTBOOK_APP_KEY = Read-SmokeSecret 'Moltbook app key'
    $env:MOLTBOOK_IDENTITY_TOKEN = Read-SmokeSecret 'Fresh identity token'
    $reportLines = & $smokeExe -live
    $smokeExit = $LASTEXITCODE
    [IO.File]::WriteAllText($reportPath, ($reportLines -join "`n"), (New-Object Text.UTF8Encoding($false)))
} finally {
    $env:MOLTBOOK_APP_KEY = $priorKey
    $env:MOLTBOOK_IDENTITY_TOKEN = $priorToken
    $priorKey = $null
    $priorToken = $null
}
if ($smokeExit -ne 0) { throw "Smoke did not pass; inspect $reportPath before considering another run" }
& $smokeExe -verify-report $reportPath
if ($LASTEXITCODE -ne 0) { throw 'Saved receipt verification failed' }
Write-Output "Report: $reportPath"
```

For a PowerShell offline rehearsal, use the same BOM-free output pattern with
`& $smokeExe` and omit credential prompts and `-live`.

## Report interpretation and limits

A successful live run reports `mode: live_identity_local_fixture`,
`live_identity_verified: true`, `identity_status: VERIFIED`, the local observation
time, source endpoint, recorded build commit, hashed identity reference, explicit
authority, and the verified receipt. Raw credentials, agent ID and upstream
profile/body are omitted. Identity references are pseudonymous hashes, not a
guarantee of anonymity. Review a report before sharing it.

`-verify-report` checks report consistency, receipt/packet/authority bindings and
the expected control-fixture hash. It needs neither credentials nor network. It
does **not** independently prove that Moltbook returned a valid identity: these
reports are not provider-signed attestations, and their hashes do not authenticate
their author. Keep the observed live run and its reviewed build revision as the
source of that claim. An offline rehearsal cannot satisfy the live acceptance
criteria in #25.

If output writing fails after an identity request, the process reports failure;
it does not retry the request. Inspect the failure before manually trying again.
The random action ID and Station's in-process replay check do not provide durable
cross-process replay protection. The smoke does not prove production availability,
an external AgentProof integration or social automation support.
