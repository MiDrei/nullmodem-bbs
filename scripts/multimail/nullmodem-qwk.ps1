# Fetches your NullModem BBS QWK mail via HTTP, opens it in MultiMail
# (https://wmcbrine.com/MultiMail/, "mm.exe") for reading/replying,
# then uploads whatever reply packet MultiMail produced.
#
# Requires: curl.exe (bundled with Windows 10/11), mm.exe (MultiMail --
# download a Windows build from https://wmcbrine.com/MultiMail/).
# Note: this script calls "curl.exe" explicitly, not "curl" -- in
# Windows PowerShell 5.1, "curl" is an alias for Invoke-WebRequest,
# which does NOT behave like real curl (e.g. -F multipart uploads).
#
# ==================== Configuration ====================
# Edit these directly, or leave them blank and set the matching
# NULLMODEM_* environment variable instead (handy for a shared
# script or a scheduled task) -- an env var always wins if both are
# set. Leaving Username/Password blank prompts for them instead.

$BbsUrl = "https://bbs.maik.ch"
$Username = ""    # e.g. "alice"
$Password = ""    # leave blank to be prompted each run
$QwkDown = ""     # leave blank for the default (%USERPROFILE%\nullmodem-qwk\incoming)
$QwkUp = ""       # leave blank for the default (%USERPROFILE%\nullmodem-qwk\outgoing)
# =========================================================

$ErrorActionPreference = "Stop"

# Windows PowerShell 5.1 sometimes defaults to an older TLS version
# than this server requires, which makes Invoke-RestMethod fail with
# an SSL/TLS handshake error further down -- force TLS 1.2 up front.
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

if ($env:NULLMODEM_URL) { $BbsUrl = $env:NULLMODEM_URL }
if ($env:NULLMODEM_USER) { $Username = $env:NULLMODEM_USER }
if (-not $Username) { $Username = Read-Host "BBS username" }
if ($env:NULLMODEM_PASS) { $Password = $env:NULLMODEM_PASS }
if (-not $Password) {
	$SecurePassword = Read-Host "BBS password" -AsSecureString
	$Password = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto(
		[System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecurePassword))
}
$WorkDir = if ($env:NULLMODEM_QWK_DIR) { $env:NULLMODEM_QWK_DIR } else { Join-Path $env:USERPROFILE "nullmodem-qwk" }
if ($env:NULLMODEM_QWK_DOWN) { $QwkDown = $env:NULLMODEM_QWK_DOWN }
if ($env:NULLMODEM_QWK_UP) { $QwkUp = $env:NULLMODEM_QWK_UP }
$InDir = if ($QwkDown) { $QwkDown } else { Join-Path $WorkDir "incoming" }
$OutDir = if ($QwkUp) { $QwkUp } else { Join-Path $WorkDir "outgoing" }
New-Item -ItemType Directory -Force -Path $InDir, $OutDir | Out-Null
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $InDir "*.qwk"), (Join-Path $InDir "*.QWK")

Write-Host "Logging in to $BbsUrl ..."
# Invoke-RestMethod (not curl.exe) here specifically -- PowerShell
# mangles a JSON string's embedded double quotes when it's passed as
# an argument to a native executable, silently corrupting the request
# body (curl.exe would then get a 400 "invalid request body" back,
# which looks like a login failure but isn't one).
try {
	$LoginResponse = Invoke-RestMethod -Uri "$BbsUrl/api/bbs/auth/login" -Method Post `
		-ContentType "application/json" `
		-Body (@{ username = $Username; password = $Password } | ConvertTo-Json)
} catch {
	Write-Error "Login request failed: $($_.Exception.Message)"
	exit 1
}
$Token = $LoginResponse.token
if (-not $Token) {
	Write-Error "Login failed -- check username/password."
	exit 1
}

Write-Host "Downloading QWK packet ..."
$Packet = Join-Path $InDir "packet.qwk"
$HttpCode = curl.exe -sS -o $Packet -w "%{http_code}" `
	-H "Authorization: Bearer $Token" `
	"$BbsUrl/api/bbs/qwk/download"

if ($HttpCode -eq "204") {
	Write-Host "No new mail. Nothing to read."
	Remove-Item -Force -ErrorAction SilentlyContinue $Packet
	exit 0
} elseif ($HttpCode -ne "200") {
	Write-Error "Download failed (HTTP $HttpCode)."
	Get-Content $Packet -ErrorAction SilentlyContinue
	exit 1
}

Write-Host "Starting MultiMail -- reply packets are saved to $OutDir"
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $OutDir "*.rep"), (Join-Path $OutDir "*.REP")
mm.exe -PacketDir $InDir -ReplyDir $OutDir $InDir

$Reply = Get-ChildItem -Path $OutDir -Filter "*.rep" -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $Reply) {
	Write-Host "No reply packet was created -- nothing to upload."
	exit 0
}

Write-Host "Uploading reply packet ($($Reply.FullName)) ..."
curl.exe -fsS -X POST "$BbsUrl/api/bbs/qwk/upload" `
	-H "Authorization: Bearer $Token" `
	-F "file=@$($Reply.FullName)"
Write-Host ""
Write-Host "Done."
