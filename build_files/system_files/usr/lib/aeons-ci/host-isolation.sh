#!/usr/bin/env bash

aeons_uid_map_contains() {
    local uid=$1 map_file=$2
    awk -v uid="$uid" \
        '$2 <= uid && uid < $2 + $3 { found=1 } END { exit found ? 0 : 1 }' \
        "$map_file"
}

aeons_is_direct_runner_cgroup() {
    local expected_parent=$1 candidate=$2 relative
    if [[ $candidate != "$expected_parent/"* ]]; then
        return 1
    fi
    relative=${candidate#"$expected_parent/"}
    [[ $relative =~ ^libpod-[0-9a-f]{64}$ ]]
}
