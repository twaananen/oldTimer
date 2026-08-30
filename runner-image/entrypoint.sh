#!/usr/bin/env bash
set -euo pipefail

cp -a \
    /opt/actions-runner/runtime/.bash_logout \
    /opt/actions-runner/runtime/.bashrc \
    /opt/actions-runner/runtime/.profile \
    /opt/actions-runner/runtime/config.sh \
    /opt/actions-runner/runtime/env.sh \
    /opt/actions-runner/runtime/run-helper.cmd.template \
    /opt/actions-runner/runtime/run-helper.sh.template \
    /opt/actions-runner/runtime/run.sh \
    /opt/actions-runner/runtime/safe_sleep.sh \
    /runner/
cp -a /opt/actions-runner/runtime/bin /runner/
for directory in externals k8s k8s-novolume; do
    ln -s "/opt/actions-runner/runtime/$directory" "/runner/$directory"
done
exec /runner/run.sh
