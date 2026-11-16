#!/bin/bash
set -euo pipefail

mkdir /source /output
cd /source
curl --fail --show-error --location --retry 3 \
    "https://github.com/trapexit/mergerfs/releases/download/${VERSION}/mergerfs-${VERSION}.tar.gz" -o source.tar.gz
echo "${SOURCE_SHA256}  source.tar.gz" | sha256sum -c -
tar -xzf source.tar.gz
cd "mergerfs-${VERSION}"
make -j2 rpm
cp rpmbuild/RPMS/x86_64/mergerfs-*.rpm /output/package.rpm
