# Host checksums

Every host artifact version under `host:` in `pkg/software/catalog/host-*.yaml` lists a
checksum per platform for each archive and each binary. Each config has one checksum, shared by
every platform, and the tool requires every platform to produce the same value.

Checksums are checked when the file is downloaded, when an archive is unpacked, and again right
before the provisioner runs an installed binary (`software.VerifyExecutables`). A binary that
came out of an archive has no published checksum upstream, so its value has to be computed by
unpacking the archive.

`task host-checksums` computes them.

## Adding a version

A maintainer runs the tool by hand on a development machine when adding or changing a host
version, and commits the result. CI does not run it, and `solo-provisioner` never computes
checksums: at install and run time it only verifies files against the committed values.

1. Write the version block with its names, URLs and algorithms, and an empty `checksum: ''`
   for every platform. Copy the block of the previous version and blank its checksums. Use the
   algorithm upstream publishes (`sha512` for `dl.k8s.io`, `sha256` elsewhere).
2. Where upstream publishes a checksum file for an archive or a binary, keep or add a
   `# from <url>` comment on that checksum line. The tool then fetches that file and requires it
   to contain the computed value.
3. Run from the repository root:

   ```bash
   task host-checksums -- -write
   ```

4. Review the diff, then run `task lint` and the unit tests.

A block skeleton, for one archive with one binary inside it:

```yaml
- name: cri-o
  versions:
    1.36.7:
      archives:
        - name: 'cri-o.{{.ARCH}}.v{{.VERSION}}.tar.gz'
          url: 'https://storage.googleapis.com/cri-o/artifacts/cri-o.{{.ARCH}}.v{{.VERSION}}.tar.gz'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: '' # from https://storage.googleapis.com/cri-o/artifacts/cri-o.amd64.v1.36.7.tar.gz.sha256sum
            arm64:
              algorithm: 'sha256'
              checksum: '' # from https://storage.googleapis.com/cri-o/artifacts/cri-o.arm64.v1.36.7.tar.gz.sha256sum
      binaries:
        - name: 'cri-o/bin/crio'
          archive: 'cri-o.{{.ARCH}}.v{{.VERSION}}.tar.gz'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: ''
            arm64:
              algorithm: 'sha256'
              checksum: ''
```

## What the tool does

- Picks the version blocks that have an empty checksum. `-all` picks every block;
  `-name` and `-version` narrow either choice.
- For each platform a block lists, downloads and unpacks the version with the same
  `pkg/software` installer code a host install uses, into a temporary directory, and hashes
  every file with the algorithm the catalog names for it.
- Prints one row per checksum: `empty` or `filled` for a blank value, `ok` for a match,
  `MISMATCH` otherwise.
- With `-write`, fills the blank values in place. Only the value on each checksum line
  changes; comments and layout stay. The filled files must still load as a catalog before
  anything is written.

The tool fills values but never adds keys, so every platform must already be listed.

It reads the catalog embedded at build time to download, so run it with `go run` (as the
task does) after editing a catalog file.

## Mismatches

A non-empty value that differs from the computed one is an error, and nothing is written.
The tool never overwrites a committed checksum. A changed value means the artifact behind a
released version changed upstream. Find out why before you accept it. To accept it, blank the
value and run `-write` again.

The same applies when a `# from <url>` file does not contain the computed value.

## Verifying committed values

```bash
task host-checksums -- -all -name helm
```

`-all` downloads every listed version for every platform, which is several GB for the whole
catalog. Narrow it with `-name` and `-version`.

Downloads and unpacked files go under `$TMPDIR` (default `/tmp`), one platform at a time. A
large artifact such as teleport needs about 1 GB there; if `/tmp` is a small tmpfs, set
`TMPDIR` to a directory on disk.

Source: `scripts/host-checksums/`. The download and hashing code is
`software.ComputeChecksums` in `pkg/software/checksum_generator.go`.
