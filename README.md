# oldtimer

oldtimer is an OCI image building on top of Bazzite/ublue-os images.

## mergerfs RPM

The image workflow first looks for an upstream RPM matching the pinned mergerfs
version and the Fedora release of the current Bazzite base. If upstream has no
matching x86_64 RPM, it reuses our matching GitHub Release asset or builds one in
an isolated Fedora container. API and checksum errors fail the build.

New fallback RPMs must pass a disposable mount/read/write/delete/unmount test.
Only main branch workflows publish them. Publication uses a draft until all assets
are uploaded, never replaces a published release, and does not change the latest
release. Cancelled draft uploads can be retried. PRs can build and test missing
packages but cannot publish them.

Release identities include the version, source checksum, Fedora release,
architecture, pinned builder digest, and build/test recipe hash. Ordinary Bazzite
and kernel updates reuse the RPM. Each release contains the RPM, SHA256 checksum,
and build provenance. Workflow artifacts only transfer files between jobs.

Update `packages/mergerfs/config.json` to change mergerfs or add a builder for a new
Fedora release. Review builder/package security updates periodically: builder
digests are pinned, but Fedora's package repositories are not snapshots. Changes
to the build/test recipe or builder digest produce a new package release.

For a local build, `just build` prepares the RPM automatically; it requires Python
3, skopeo, Docker (including `/dev/fuse` access for a fallback build), and Podman.
The prepared RPM is ignored by Git. The image build verifies its checksum and
requires mergerfs to be installed and runnable. It uses the exact Bazzite digest
inspected during package selection.

Run resolver regression tests with `python3 -m unittest discover -s tests -v`.
