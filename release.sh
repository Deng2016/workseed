#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$SCRIPT_DIR"

VERSION=${1:-$(git describe --tags --always 2>/dev/null || printf 'dev')}
VERSION=${VERSION#v}
if [[ ! "$VERSION" =~ ^[0-9A-Za-z._-]+$ ]]; then
  printf '错误：版本号只能包含字母、数字、点、下划线和连字符。\n' >&2
  exit 1
fi

OUTPUT_DIR="$SCRIPT_DIR/release"
WINDOWS11_TARGET_DIR="/mnt/d/AppData/workseed"
WINDOWS11_PACKAGE="workseed-${VERSION}-windows11-amd64.zip"
STAGING_DIR=$(mktemp -d /tmp/workseed-release.XXXXXX)

cleanup() {
  case "$STAGING_DIR" in
    /tmp/workseed-release.*) rm -rf -- "$STAGING_DIR" ;;
  esac
}
trap cleanup EXIT

for command_name in go npm tar sha256sum; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf '错误：缺少构建命令 %s。\n' "$command_name" >&2
    exit 1
  fi
done
if ! command -v zip >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
  printf '错误：生成 Windows ZIP 包需要 zip 或 python3。\n' >&2
  exit 1
fi

printf '==> 清理之前的构建记录\n'
case "$OUTPUT_DIR" in
  "$SCRIPT_DIR/release") rm -rf -- "$OUTPUT_DIR" ;;
  *)
    printf '错误：拒绝清理非预期目录 %s。\n' "$OUTPUT_DIR" >&2
    exit 1
    ;;
esac
mkdir -p "$OUTPUT_DIR"

printf '==> 构建前端\n'
npm --prefix web run build

printf '==> 运行 Go 测试\n'
go test ./...

build_linux() {
  local package_name="workseed-${VERSION}-linux-amd64"
  local stage="$STAGING_DIR/$package_name"
  mkdir -p "$stage"
  printf '==> 构建 Linux amd64\n'
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$stage/workseed" ./cmd/workseed
  cp README.md "$stage/README.md"
  tar -C "$STAGING_DIR" -czf "$OUTPUT_DIR/${package_name}.tar.gz" "$package_name"
}

create_windows_zip() {
  local stage=$1
  local archive=$2
  rm -f -- "$archive"
  if command -v zip >/dev/null 2>&1; then
    (cd "$stage" && zip -q "$archive" workseed.exe README.md)
  else
    (cd "$stage" && python3 -m zipfile -c "$archive" workseed.exe README.md)
  fi
}

build_windows() {
  local windows_version=$1
  local package_name="workseed-${VERSION}-windows${windows_version}-amd64"
  local stage="$STAGING_DIR/$package_name"
  mkdir -p "$stage"
  printf '==> 构建 Windows %s amd64\n' "$windows_version"
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w -H=windowsgui' -o "$stage/workseed.exe" ./cmd/workseed
  cp README.md "$stage/README.md"
  create_windows_zip "$stage" "$OUTPUT_DIR/${package_name}.zip"
}

build_linux
build_windows 10
build_windows 11

packages=(
  "workseed-${VERSION}-linux-amd64.tar.gz"
  "workseed-${VERSION}-windows10-amd64.zip"
  "$WINDOWS11_PACKAGE"
)
(
  cd "$OUTPUT_DIR"
  sha256sum "${packages[@]}" > SHA256SUMS
)

if [[ -d "$WINDOWS11_TARGET_DIR" ]]; then
  cp -- "$OUTPUT_DIR/$WINDOWS11_PACKAGE" "$WINDOWS11_TARGET_DIR/"
  printf '==> 已复制 Windows 11 包到 %s\n' "$WINDOWS11_TARGET_DIR"
else
  printf '==> 目标目录 %s 不存在，跳过复制 Windows 11 包\n' "$WINDOWS11_TARGET_DIR"
fi

printf '\n发布完成：%s\n' "$OUTPUT_DIR"
printf '  %s\n' "${packages[@]}" SHA256SUMS
