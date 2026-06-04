# #616 — Replace VM 9p shared mount with rsync-based sync

## Summary

**Problem.** The UTM VM mounted the host source tree via virtio-9p at `/mnt/solo-weaver`, configured in
`vm:configure:ssh`. The 9p mount is bound to whichever host directory UTM had registered the first time the VM was
provisioned, so switching git worktrees on the host left the VM looking at the original tree — newly added files were
invisible despite a successful host build. virtio-9p on macOS+UTM was also intermittently flaky.

**Solution.** Drop the 9p `/etc/fstab` entry; instead, populate `/mnt/solo-weaver` via a new `task vm:sync` that
`rsync -az --delete`s the host worktree into the VM. The in-VM path stays `/mnt/solo-weaver`, so ~52 references in
tasks, UAT scripts, docs, and `scripts/debug-vm-run.sh` are unchanged. `vm:sync` is wired as a dep on the host-side
tasks that need fresh code in the VM. CI is unaffected — `.github/workflows/zxc-uat-test.yaml` already rsyncs +
symlinks `/mnt/solo-weaver`, so local now matches CI.

**Upgrade path for existing dev VMs:** `task vm:reset` once. Clones from the golden image, so the legacy 9p fstab
entry is left behind on the old working VM (which gets destroyed by `vm:reset`).

## Changed files

| File | Change |
|---|---|
| `taskfiles/vm.yaml` | `vm:configure:ssh`: drop 9p fstab block, `chown /mnt/solo-weaver` to `provisioner`. Add new `vm:sync` task. Add `vm:sync` dep to `vm:debug:test`, `vm:debug:app`, `vm:configure:tools`. Rewrite "restart UTM" banner in `vm:download:golden` to drop the obsolete "set up shared folder" wording. |
| `taskfiles/tests.yaml` | Add `vm:sync` to deps of `vm:test:unit` and `vm:test:integration`. |
| `taskfiles/alloy.yaml` | Add `vm:sync` to deps of `vm:alloy:start`, `vm:alloy:stop`, `vm:alloy:clean`. |
| `taskfiles/teleport.yaml` | Add `vm:sync` to deps of `vm:teleport:start`, `vm:teleport:stop`, `vm:teleport:clean`. |
| `docs/dev/quickstart.md` | Drop obsolete "set up shared folder" wording. Add new section "Syncing host changes into the VM" explaining `task vm:sync`. Renumber section 2 → 3. |
| `docs/dev/acceptance-tests.md` | Add `task vm:sync` to the prerequisites bullet list. |
| `docs/claude/plans/00616-replace-9p-with-rsync-sync.md` | Implementation plan (drafted before code changes per project convention). |
| `docs/claude/reviews/00616-replace-9p-with-rsync-sync.md` | This file. |

## Code review checklist

- [ ] `vm:configure:ssh` no longer writes a 9p line to `/etc/fstab`; `/mnt/solo-weaver` is just `mkdir -p` + `chown -R provisioner:provisioner`.
- [ ] `vm:sync` declares `deps: [vm:start]`, uses `{{.GET_VM_IP}}`, `{{.SSH_PRIVATE_KEY}}`, `{{.SSH_OPTS}}`, `{{.VM_USER}}` — same conventions as the surrounding tasks.
- [ ] `vm:sync`'s exclude list omits `bin/` and `vendor/` on purpose (local builds happen on the host; CI excludes them because it rebuilds in-VM).
- [ ] `--delete` is on in `vm:sync` — confirm no in-VM build artifacts under `/mnt/solo-weaver` would be wiped by it. (Builds put output in `bin/` which IS synced from host, so `--delete` only nukes stale files from prior host states.)
- [ ] Every host-side task that does work expecting fresh code in `/mnt/solo-weaver` declares `vm:sync` as a dep: `vm:test:unit`, `vm:test:integration`, `vm:alloy:{start,stop,clean}`, `vm:teleport:{start,stop,clean}`, `vm:debug:{test,app}`, `vm:configure:tools`.
- [ ] `task vm:ssh` and `task vm:ssh:proxy` intentionally do NOT depend on `vm:sync` — they're meant for interactive use, and users may want to SSH in without forcing a sync.
- [ ] No reference to `9p`, `virtio-9p`, or the "shared folder" UTM wording remains in `taskfiles/` or `docs/dev/`.
- [ ] In-VM `/mnt/solo-weaver` paths in `taskfiles/uat.yaml`, `taskfiles/alloy.yaml`, `taskfiles/teleport.yaml`, `scripts/debug-vm-run.sh`, `docs/dev/acceptance-tests.md`, `docs/dev/quickstart.md` are unchanged — the in-VM path is identical pre- and post-PR.

## Sanity check (host)

```bash
# 1. Surface any lingering 9p references
rg -n '9p|virtio-9p|shared folder' taskfiles/ docs/dev/
# Expected: no hits (the only `virtio` hits should be in .github/workflows/* for the QEMU NIC/disk).

# 2. Confirm every host-side vm:* task that runs against /mnt/solo-weaver declares vm:sync
rg -n 'vm:sync' taskfiles/
# Expected: vm.yaml (definition + deps), tests.yaml, alloy.yaml, teleport.yaml — at least 10 hits.
```

## Manual UAT (UTM VM)

1. **Fresh VM bring-up — no 9p anywhere.**

   ```bash
   task vm:reset
   task vm:ssh
   # inside the VM:
   grep -E '/mnt/solo-weaver' /etc/fstab
   # Expected: no output (or non-zero exit), the legacy 9p line is gone.
   mountpoint /mnt/solo-weaver
   # Expected: "/mnt/solo-weaver is not a mountpoint" — it's a plain directory now.
   ls -ld /mnt/solo-weaver
   # Expected: owned by provisioner:provisioner.
   ls /mnt/solo-weaver
   # Expected: empty until first `task vm:sync` from the host.
   exit
   ```

2. **`task vm:sync` populates the tree.**

   ```bash
   task vm:sync
   # Expected output (abridged):
   #   📤 Syncing /Users/.../solo-weaver-wt/00616-... → provisioner@<vm-ip>:/mnt/solo-weaver/ ...
   #   sending incremental file list
   #   ... (rsync progress) ...
   #   ✓ Sync complete
   task vm:ssh
   # inside the VM:
   ls /mnt/solo-weaver/Taskfile.yaml /mnt/solo-weaver/cmd /mnt/solo-weaver/bin
   # Expected: all present.
   exit
   ```

3. **Worktree switch is visible after `task vm:sync`.** This is the core regression scenario from issue #616.

   ```bash
   # In worktree A:
   echo "// scratch-A" >> internal/state/state.go
   task vm:sync
   task vm:ssh
   # inside the VM:
   tail -1 /mnt/solo-weaver/internal/state/state.go
   # Expected: "// scratch-A"
   exit

   # Now switch to a different worktree directory (e.g. cd into another solo-weaver-wt/<branch>/):
   cd /path/to/other/worktree
   task vm:sync
   task vm:ssh
   # inside the VM:
   tail -1 /mnt/solo-weaver/internal/state/state.go
   # Expected: the OTHER worktree's last line — the "// scratch-A" appended in worktree A is GONE.
   # This is what was broken before — 9p kept pointing at worktree A regardless of where the host was.
   exit
   ```

4. **`task vm:test:unit` auto-syncs and passes.**

   ```bash
   echo "// scratch-2" >> internal/state/state.go
   task vm:test:unit
   # Expected: rsync runs as a dep, then unit tests execute inside the VM and pass.
   ```

5. **UAT lifecycle still passes inside the VM.**

   ```bash
   task build:cli GOOS=linux GOARCH=arm64
   task vm:sync
   task vm:ssh:proxy
   # inside the VM:
   task uat:lifecycle
   # Expected: setup → core upgrades → teardown all green, identical to pre-PR behavior.
   ```

## CI verification

No workflow changes. The existing rsync-based step in `.github/workflows/zxc-uat-test.yaml` (lines 380-405) and the
`ln -sfn /home/provisioner/solo-weaver /mnt/solo-weaver` symlink (line 472-473) keep working as-is. Confirm the
`uat-lifecycle` workflow stays green on this PR.

## Risks & rollback

- **Forgetting `task vm:sync` after host edits.** Mitigated by `vm:sync` deps on the hot-path tasks. Plain `task vm:ssh`
  is the escape hatch where the user must remember.
- **Initial sync slower than 9p (which was zero-copy).** First sync rsyncs ~100 MB minus excludes; subsequent syncs are
  incremental. Acceptable trade for the correctness + reliability win.
- **Existing dev VMs with a stale 9p fstab line.** Resolved by `task vm:reset` (which clones from the golden image, so
  the working VM is destroyed). Devs who don't reset will see weirdness — `task vm:sync` would write into a 9p-mounted
  directory, which actually works but defeats the purpose. Call this out in the PR body.
- **Rollback:** revert this PR. The 9p fstab block returns on next `vm:reset`. No data loss; the VM's own disk is
  unaffected because `/mnt/solo-weaver` is just a mount point / sync target.
