#!/bin/bash
set -euo pipefail

WORK=$(mktemp -d)
trap 'if mountpoint -q "$WORK/pool"; then umount "$WORK/pool"; fi; rm -rf "$WORK"' EXIT
mkdir "$WORK/a" "$WORK/b" "$WORK/pool"
echo first > "$WORK/a/first"
echo second > "$WORK/b/second"
mount -t fuse.mergerfs -o nonempty,allow_other,use_ino,cache.files=partial,moveonenospc=true,dropcacheonclose=true,minfreespace=0 \
    "$WORK/a:$WORK/b" "$WORK/pool"
test "$(cat "$WORK/pool/first")" = first
test "$(cat "$WORK/pool/second")" = second
echo written > "$WORK/pool/new"
test "$(cat "$WORK/pool/new")" = written
test -f "$WORK/a/new" || test -f "$WORK/b/new"
rm "$WORK/pool/new"
test ! -e "$WORK/a/new" && test ! -e "$WORK/b/new"
umount "$WORK/pool"
echo 'PASS: mergerfs mount, merged reads, write, delete, unmount'
