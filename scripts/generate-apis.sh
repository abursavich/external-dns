#!/usr/bin/env bash
set -Eeuo pipefail

export GO111MODULE=on

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." &>/dev/null && pwd -P)
PACKAGE_NAME=sigs.k8s.io/external-dns
VERSION=$(go list -m -f '{{.Version}}' k8s.io/client-go)

pushd () { command pushd "$@" > /dev/null; }
popd  () { command popd  "$@" > /dev/null; }

if [[ ! -d /tmp/code-generator ]]; then
  git clone https://github.com/kubernetes/code-generator.git /tmp/code-generator
fi
pushd /tmp/code-generator
git checkout --quiet $VERSION
go get ./...
popd

TMP_DIR=$(mktemp -d -t external-dns-XXXXXXXXXX)
trap '{ rm --force --recursive -- "$TMP_DIR"; }' EXIT

mkdir --parents "$TMP_DIR/$PACKAGE_NAME/third_party"
cp --recursive --link "$REPO_ROOT/third_party" "$TMP_DIR/$PACKAGE_NAME"
cp "$REPO_ROOT/go.mod" "$REPO_ROOT/go.sum" "$TMP_DIR/$PACKAGE_NAME"

pushd "$TMP_DIR/$PACKAGE_NAME"

echo "Generating code for Gloo..."
/tmp/code-generator/generate-groups.sh \
  all \
  "$PACKAGE_NAME/third_party/solo.io" \
  "$PACKAGE_NAME/third_party/solo.io/apis" \
  "gloo:v1" \
  --go-header-file "${REPO_ROOT}/third_party/solo.io/boilerplate.go.txt" \
  --output-base "$TMP_DIR"

echo "Generating code for ProjectContour..."
/tmp/code-generator/generate-groups.sh \
  all \
  "$PACKAGE_NAME/third_party/projectcontour.io" \
  "$PACKAGE_NAME/third_party/projectcontour.io/apis" \
  "contour:v1beta1 projectcontour:v1" \
  --go-header-file "${REPO_ROOT}/third_party/projectcontour.io/boilerplate.go.txt" \
  --output-base "$TMP_DIR"

popd

cp --recursive --link --force "$TMP_DIR/$PACKAGE_NAME/third_party" "$REPO_ROOT"
