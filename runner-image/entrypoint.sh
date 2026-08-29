#!/usr/bin/env bash
set -euo pipefail

cp -a /opt/actions-runner/runtime/. /runner/
exec /runner/run.sh
