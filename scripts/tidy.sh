#!/usr/bin/env bash
set -euo pipefail

# Keep go.mod/go.sum normalized and verify module graph.
go mod tidy
go mod verify
