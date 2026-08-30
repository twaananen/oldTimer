#!/usr/bin/env bash

aeons_uid_map_contains() {
    local uid=$1 map_file=$2
    awk -v uid="$uid" \
        '$2 <= uid && uid < $2 + $3 { found=1 } END { exit found ? 0 : 1 }' \
        "$map_file"
}
