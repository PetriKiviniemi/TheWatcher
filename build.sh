#!/usr/bin/env bash
# Builds build/TonttivahtiSetup.exe on Linux or macOS. Needs Go, Node, makensis and (for signing) osslsigncode.
# Signing is optional: set SIGN_PFX to a .pfx code-signing certificate and SIGN_PASS_FILE to a file holding its password.
# SIGN_TIMESTAMP_URL overrides the timestamp server; set it empty to sign without a timestamp (testing only).
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${VERSION:-1.0.0}"

sign() {
  [[ -n "${SIGN_PFX:-}" ]] || return 0
  : "${SIGN_PASS_FILE:?set SIGN_PASS_FILE to a file containing the certificate password}"
  local timestamp_url="${SIGN_TIMESTAMP_URL-http://timestamp.digicert.com}"
  osslsigncode sign -pkcs12 "$SIGN_PFX" -readpass "$SIGN_PASS_FILE" -n "Tonttivahti" -h sha256 \
    ${timestamp_url:+-t "$timestamp_url"} -in "$1" -out "$1.signed"
  mv "$1.signed" "$1"
}

# makensis calls back with --sign to sign the uninstaller it generates.
if [[ "${1:-}" == "--sign" ]]; then
  sign "$2"
  exit
fi

npm --prefix web ci
npm --prefix web run build
go test ./...

mkdir -p build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui -s -w" -o build/tonttivahti.exe .
sign build/tonttivahti.exe

makensis -DVERSION="$VERSION" ${SIGN_PFX:+-DSIGN_CMD="$PWD/build.sh --sign"} installer.nsi
sign build/TonttivahtiSetup.exe

echo "Built build/TonttivahtiSetup.exe${SIGN_PFX:+ (signed)}"
