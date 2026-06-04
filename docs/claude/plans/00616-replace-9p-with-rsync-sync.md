# #616 — Replace VM 9p shared mount with rsync-based sync for `/mnt/solo-weaver`

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/616
> **Story branch:** `00616-replace-9p-with-rsync-sync`
> **PR base:** `main` (branched from `origin/main` @ `f07505c`)
> **PR closes:** #616

## Summary

The UTM VM currently sees the host source tree via virtio-9p at `/mnt/solo-weaver`, configured via an `/etc/fstab` entry in `vm:configure:ssh`. virtio-9p on macOS+UTM is flaky (stalls, permission glitches) and — more importantly — it silently keeps pointing at the worktree path that was active the first time the VM was provisioned. Switching to a sibling worktree under `solo-weaver-wt/` leaves the VM looking at the old tree, so newly added files are invisible despite a successful host build.

This PR replaces the 9p mount with a one-way rsync from host → VM, keeping `/mnt/solo-weaver` as the in-VM path so the ~52 references in tasks, UAT scripts, docs, and `scripts/debug-vm-run.sh` stay untouched. A new `task vm:sync` wraps the rsync; it is wired as a dep on the host-side tasks that expect fresh code in the VM (`vm:test:unit`, `vm:test:integration`, `vm:alloy:start`, `vm:teleport:start`, `vm:configure:tools`, `vm:debug:test`, `vm:debug:app`). CI is unaffected — `.github/workflows/zxc-uat-test.yaml` already rsyncs to `/home/provisioner/solo-weaver/` and symlinks `/mnt/solo-weaver`, so local now converges on the same shape.

## Problem

Current state in `taskfiles/vm.yaml:437-446` (inside `vm:configure:ssh`):

```bash
echo 'Mounting shared directory...'
sudo mkdir -p /mnt/solo-weaver
if ! grep -q '/mnt/solo-weaver' /etc/fstab; then
  echo 'share   /mnt/solo-weaver   9p  trans=virtio,version=9p2000.L,rw,nofail 0   0' \
    | sudo tee -a /etc/fstab >/dev/null
fi
sudo mount -a || true
```

Failure modes from the issue:

1. **Worktree switching is invisible to the VM.** The `share` tag in the 9p mount is bound to whichever host directory UTM had registered when the VM was first provisioned. Switching to another `solo-weaver-wt/<branch>/` worktree on the host does not retarget the mount, so the VM keeps building against the original tree.
2. **virtio-9p is intermittently flaky on macOS+UTM** — stalls and permission glitches, especially on restricted networks.

CI already avoids 9p (`.github/workflows/zxc-uat-test.yaml:380-405` rsyncs to `/home/provisioner/solo-weaver/` and symlinks `/mnt/solo-weaver`), so this PR closes the local↔CI divergence too.

## Decisions

| Question | Decision |
|---|---|
| Keep the in-VM path as `/mnt/solo-weaver`? | **Yes.** Issue explicitly says minimize churn across ~52 references. A pure plumbing change. |
| One-way or two-way sync? | **One-way (host → VM).** Source of truth is the host worktree. Two-way needs conflict resolution that's not needed here. |
| `rsync --delete`? | **Yes.** Without it, files removed on a branch switch linger in the VM and confuse builds. Build artifacts that live outside `/mnt/solo-weaver` (system Go, `/tmp/*`) are unaffected. |
| Should `vm:sync` run automatically on `vm:start`? | **No.** Make it explicit. The user invokes `task vm:sync` after worktree switches; task-level deps cover the hot-path test/UAT tasks. Auto-syncing on every `vm:start` would surprise users who just want to SSH into a running VM. |
| Clean up existing 9p fstab entries on already-provisioned VMs? | **Yes.** `vm:configure:ssh` now removes any `^.*\s/mnt/solo-weaver\s.*9p` line from `/etc/fstab` and unmounts the path if currently a 9p mountpoint, then chowns it to `provisioner`. Safe to re-run. |
| Same rsync excludes as CI? | **Mostly different.** CI excludes `vendor/` and `bin/` because it rebuilds inside the VM; locally the dev typically builds on the host (`task build:cli GOOS=linux GOARCH=arm64`) and expects `bin/solo-provisioner-linux-arm64` to be visible in the VM. Local excludes: `.git`, `.ssh`, `*.iso`, `*.img`, `*.qcow2`, `*.log`, `.DS_Store`. Keep `vendor/` and `bin/`. |
| Use `{{.VM_USER}}` (provisioner) as the rsync target? | **Yes.** Existing tasks SSH as `provisioner` with `{{.SSH_PRIVATE_KEY}}`/`{{.SSH_OPTS}}`. `vm:configure:ssh` chowns `/mnt/solo-weaver` to `provisioner:provisioner` so plain rsync writes without sudo. |

## Scope

### `taskfiles/vm.yaml`

- [ ] In `vm:configure:ssh` (the inner SSH heredoc at lines ~437-446): replace the 9p block with: remove any `9p` line for `/mnt/solo-weaver` from `/etc/fstab`, unmount the path if currently a 9p mountpoint, ensure `/mnt/solo-weaver` exists as a plain directory, `chown -R provisioner:provisioner /mnt/solo-weaver`.
- [ ] Add new task `vm:sync` (deps `[vm:start]`) that runs `rsync -az --delete` from `./` (host cwd) → `provisioner@<vm-ip>:/mnt/solo-weaver/` with the excludes listed above, using `{{.SSH_PRIVATE_KEY}}` / `{{.SSH_OPTS}}` for the SSH transport, and the existing `{{.GET_VM_IP}}` helper for IP resolution.
- [ ] Add `vm:sync` as a dep to `vm:configure:tools` (so its `cd /mnt/solo-weaver && task install:go` finds the tree on first-time setup).
- [ ] Add `vm:sync` as a dep to `vm:debug:test` and `vm:debug:app` (they SSH in and run dlv against `/mnt/solo-weaver`).

### `taskfiles/tests.yaml`

- [ ] Add `vm:sync` to the `deps` of `vm:test:unit`.
- [ ] Add `vm:sync` to the `deps` of `vm:test:integration`.

### `taskfiles/alloy.yaml`

- [ ] Add `vm:sync` to the `deps` of `vm:alloy:start` (and optionally `vm:alloy:stop` / `vm:alloy:clean` — both ssh in and `cd /mnt/solo-weaver`).

### `taskfiles/teleport.yaml`

- [ ] Add `vm:sync` to the `deps` of `vm:teleport:start` (and optionally `vm:teleport:stop` / `vm:teleport:clean`).

### `docs/dev/quickstart.md`

- [ ] Add `rsync` to the requirements list (it's already listed — keep). Add a "Syncing host changes into the VM" subsection explaining that `task vm:sync` is required after host edits or worktree switches.
- [ ] Update the "Setup VM" section to remove the wording about the UTM shared folder restart prompt that's currently in `vm:download:golden` ("👉 Please restart UTM to reload VMs and set up the shared solo-weaver directory"). That message in `taskfiles/vm.yaml:124` and `:190` becomes misleading once 9p is gone; rewrite to point at `task vm:sync`. *(Note: this is a `vm.yaml` change too, not just docs — moving the bullet here for visibility.)*

### `docs/dev/acceptance-tests.md`

- [ ] Add `task vm:sync` to the prerequisites block (line 28-32) after `task vm:start` / `task proxy:start` / `task build:cli`.

## Out of scope

- Removing or renaming `/mnt/solo-weaver` as the in-VM path. Issue explicitly preserves it.
- Changes to `.github/workflows/zxc-uat-test.yaml` or `.github/workflows/zxc-integration-test.yaml` — they already use rsync; the virtio references there are for the disk/NIC, not 9p.
- File-watcher / continuous sync (e.g. `fswatch` + auto-rsync). Manual `task vm:sync` is the agreed UX. Easy to add later if needed.
- Two-way sync or in-VM editing workflow.
- Pruning the existing `bin/` from sync (CI does, local doesn't) — see decision above.

## Test plan

- [ ] Unit: no code under `internal/` / `pkg/` / `cmd/` changes, so no Go unit tests are affected. `task lint` runs as the formatting gate (project rule).
- [ ] Manual UAT (UTM VM):
  - [ ] `task vm:reset` against a fresh golden image → succeeds with no 9p in `/etc/fstab` and `/mnt/solo-weaver` is a plain directory owned by `provisioner`.
  - [ ] `task vm:test:unit` → triggers `vm:sync`, then unit tests pass inside the VM.
  - [ ] Worktree switch test: edit a file (`echo "// scratch" >> internal/state/some_file.go`) in this worktree → switch to a different worktree directory on the host that does NOT have that change → from there run `task vm:sync` → SSH in and confirm `/mnt/solo-weaver/internal/state/some_file.go` matches the new worktree's content (no leftover `// scratch`). This is the worktree-switching scenario from the issue.
  - [ ] `task vm:ssh:proxy` → inside VM: `task uat:lifecycle`. Should pass.
  - [ ] `grep -q /mnt/solo-weaver /etc/fstab` inside the VM after `vm:reset` → returns nothing.
- [ ] CI: unchanged workflows — confirm the existing `zxc-uat-test.yaml` and `zxc-integration-test.yaml` runs stay green.

## Risks / rollbacks

- **Initial sync slow vs 9p.** First `task vm:sync` rsyncs the whole tree (~100 MB minus excludes). Subsequent syncs are incremental and fast. Acceptable trade.
- **Disk usage doubles.** Files now exist on host AND in VM (vs 9p where the VM read host files directly). ~100 MB extra in the VM disk — negligible vs the multi-GB VM image.
- **Forgetting `task vm:sync` after edits.** Mitigated by wiring deps on the hot-path test/UAT tasks. The escape hatch is `task vm:ssh` → which won't auto-sync. Documented in quickstart.
- **Pre-existing 9p mount on a dev's VM.** `vm:configure:ssh` cleans up fstab + unmounts. If a dev does not re-run `vm:reset`, their VM keeps the old 9p mount until they do — `task vm:sync` itself would just rsync over the 9p mount, which... actually works, but defeats the purpose. Mitigation: call out in the PR description that existing devs should run `task vm:reset` once.
- **Rollback:** revert the PR; the 9p fstab entry comes back; `vm:reset` re-mounts as before. No data loss.
