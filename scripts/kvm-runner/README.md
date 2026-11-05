# KVM runner host scripts

Host-side scripts for the self-hosted `kvm-runner` machine, where several runner services
share one disk and each KVM job boots a QEMU VM. They live in the repository so changes get
reviewed, but they are installed by hand on the host; the workflows cannot run them.

## Why these exist

If the runners' disk fills, a runner worker dies mid-job and its `Cleanup VM` step never runs.
The QEMU process, started with `nohup`, keeps its disk overlay open and holds the space. The
`kvm-vm-up` sweep cannot recover from this: checkout fails with `No space left on device` before
the action runs. A full disk can also leave runner files empty (for example `runsvc.sh`, which
makes systemd fail with `Exec format error`).

Three layers handle this:

| Layer | Where | What it does |
|---|---|---|
| VM slots | `kvm-vm-up` action | At most `max-concurrent-vms` (default 4) VMs run at once on the host, so the disk cannot fill from live jobs. The slot is an `flock` that QEMU itself holds, so it is freed whenever QEMU exits. |
| Job-started hook | `job-started.sh` on the host | Runs before checkout and stops/removes orphaned VMs, so a full disk cannot block cleanup. |
| Restart script | `restart.sh` on the host | Manual recovery: stops runners, kills VMs, removes their directories, repairs empty `runsvc.sh`, starts runners with retries. |

Size the slots so `max-concurrent-vms * vm-disk-gb` plus the host's own usage fits the disk.
The default is 4 slots of 20 GB for the 200 GB disk (4 x 20 + about 30 GB for the host = 110 GB). Six would fit the disk too (about 150 GB), but the host has 8 vCPUs and about 30 GB RAM, so measure load and memory with 4 VMs running before raising it.

## Installing the job-started hook

Runners only accept a hook script with a file extension, owned by the runner user.

```bash
sudo install -m 755 -o github-runner -g github-runner scripts/kvm-runner/job-started.sh /usr/local/sbin/weaver-job-started.sh

for dir in /home/github-runner/actions-runner*/; do
  grep -q '^ACTIONS_RUNNER_HOOK_JOB_STARTED=' "${dir}.env" 2>/dev/null ||
    echo 'ACTIONS_RUNNER_HOOK_JOB_STARTED=/usr/local/sbin/weaver-job-started.sh' | sudo tee -a "${dir}.env" >/dev/null
done

./restart.sh   # the runners read .env at start
```

The hook's output appears in each job's "Set up job" log, prefixed `[weaver-job-started]`.

## Installing the restart script

```bash
sudo install -m 755 scripts/kvm-runner/restart.sh /home/github-runner/actions-runner/restart.sh
```

`restart.sh` stops running jobs. Use it for recovery, not routinely.
