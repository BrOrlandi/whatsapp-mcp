#!/usr/bin/env bash
# Prepares the source of the Evolution Go build this stack runs: the official
# 0.7.2 with the patches in patches/ applied on top. See README.md for why.
#
#   deploy/evolution/prepare.sh <empty directory>
#
# then `docker build <directory>` builds it with Evolution's own Dockerfile.
set -euo pipefail

UPSTREAM=https://github.com/evolution-foundation/evolution-go.git
# The commit tag 0.7.2 points at, so a moved tag cannot change what is built.
BASE=9337afc47e10b86cc896a6f432240e40fee95dd1

dest=${1:?usage: prepare.sh <empty directory>}
here=$(cd "$(dirname "$0")" && pwd)

git init -q "$dest"
git -C "$dest" fetch -q --depth 1 "$UPSTREAM" "$BASE"
git -C "$dest" -c advice.detachedHead=false checkout -q FETCH_HEAD
git -C "$dest" -c user.name=build -c user.email=build@localhost am -q "$here"/patches/*.patch
git -C "$dest" log --oneline -n 3
