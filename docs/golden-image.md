# Create the Golden Images

If this is your first time setting up, follow these steps:

##### Debian Installation

1. Start a new VM in UTM:
    - Select **Virtualize** → **Linux**.
    - Boot from the Debian ISO:  
      [Debian 13.1.0 ARM64 Netinst](https://cdimage.debian.org/cdimage/archive/13.1.0/arm64/iso-cd/debian-13.1.0-arm64-netinst.iso).
        - If the link is outdated, find the latest
          at [Debian CD Image Archive](https://www.debian.org/CD/http-ftp/#stable).
    - Storage: 64 GiB (default).
    - Shared Directory: set to your `solo-weaver` project root.
    - Network: **Bridged (Advanced)** on your Wi-Fi interface (e.g., `en0`).
    - Name the VM: `solo-weaver-debian-golden`.

2. Install Linux in the VM:

- Start the golden image installation.
- Keep all default selections except:
    - Language: **English (US)**.
    - Location: your country (e.g., **Australia**).
    - Locale: **en_US.UTF-8**.
    - Keyboard: **American English**.
    - Hostname: keep default `debian`.
    - Domain: leave empty.
    - Root password: leave empty.
    - User:
        - Full name: `provisioner`
        - Username: `provisioner`
        - Password: `provisioner`
    - Clock: default selection.
    - Partitioning:
        - Guided – use entire disk
        - All files in one partition
        - Write changes to disk
    - Package mirror: choose a local Debian mirror (e.g., `mirror.aarnet.edu.au`).
    - Software selection: keep default
- Remove the ISO from UTM (**CD/DVD → Clear**) before reboot.
- Select **Continue** and boot into Debian.

✅ At this point, your **debian golden image** is ready.

##### Ubuntu Installation

1. Start a new VM in UTM:
    - Select **Virtualize** → **Linux**.
    - Boot from the Ubuntu ISO:  
      [Ubuntu 22.04.5 LTS ARM64](https://cdimage.ubuntu.com/releases/jammy/release/ubuntu-22.04.5-live-server-arm64.iso).
    - Storage: 64 GiB (default).
    - Shared Directory: set to your `solo-weaver` project root.
    - Network: **Bridged (Advanced)** on your Wi-Fi interface (e.g., `en0`).
    - Name the VM: `solo-weaver-ubuntu-golden`.

2. Install Linux in the VM:

- Start the golden image installation.
- Keep all default selections except:
    - Language: **English (US)**.
    - Type of installation: **Ubuntu Server (minimized)**.
    - User:
        - Full name: `provisioner`
        - Server name: `ubuntu`
        - Username: `provisioner`
        - Password: `provisioner`
- Remove the ISO from UTM (**CD/DVD → Clear**) before reboot.
- Reboot and log in with:
    - Username: `provisioner`
    - Password: `provisioner`
- Install QEMU Guest Agents:
    ```sh
    sudo apt update
    sudo apt install qemu-guest-agent -y
    ```

✅ At this point, your **ubuntu golden image** is ready.

##### Install dependencies and clean up

1. Start the VM and log in as provisioner
2. Run the following script inside the VM to set up the environment
    ```bash
    set -e
    sudo apt-get update
    sudo apt-get install -y git curl rsync build-essential binutils-aarch64-linux-gnu binutils-gold gcc libc6-dev
    curl -1sLf 'https://dl.cloudsmith.io/public/task/task/setup.deb.sh' | sudo -E bash
    sudo apt-get update
    sudo apt-get install -y task
    sudo apt install -y vim ca-certificates htop net-tools python3 python3-pip qemu-guest-agent
    curl -sSLO https://go.dev/dl/go1.25.2.linux-arm64.tar.gz
    sudo mkdir -p /usr/local/go
    sudo tar -C /usr/local -xzf go1.25.2.linux-arm64.tar.gz
    rm -f go1.25.2.linux-arm64.tar.gz
    echo 'PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' | sudo tee /etc/profile.d/go.sh >/dev/null 2>&1
    source /etc/profile.d/go.sh && go install github.com/go-delve/delve/cmd/dlv@v1.25.2
    source /etc/profile.d/go.sh && /usr/local/go/bin/go env -w GOTOOLCHAIN=go1.25.0+auto
    source /etc/profile.d/go.sh && sudo /usr/local/go/bin/go env -w GOTOOLCHAIN=go1.25.0+auto
    sudo apt clean
    sudo journalctl --vacuum-time=1s
    sudo rm -rf /tmp/*
    sudo rm -rf /var/tmp/*
    sudo shutdown -h now
    ```
3. The VM will shut down automatically after the script completes.
4. In UTM, go to **Virtual Machine** → **Share...** to save the golden image (.utm file).
5. Name the template `solo-weaver-debian-golden` and save it.
6. Since .utm file is a directory, we need to compress it before uploading:
    ```bash
    cd /path/to/saved/utm/file
    tar -czvf solo-weaver-{{.TYPE}}-golden.utm.tar.gz solo-weaver-{{.TYPE}}-golden.utm
    ```
7. Upload this to the shared bucket `gs://solo-weaver/solo-weaver-debian-golden.utm.tar.gz` for use in provisioning new VMs.
