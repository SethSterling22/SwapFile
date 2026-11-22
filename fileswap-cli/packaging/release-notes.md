## Install the CLI

```bash
curl -fsSL https://raw.githubusercontent.com/SethSterling22/SwapFile/main/fileswap-cli/install.sh | sh
```

Debian/Ubuntu, Arch or any Linux, amd64/arm64. It installs the newest release with your package manager, checks `SHA256SUMS`, deletes the download and restarts the `fileswap` service if it runs. Run it again to update. For **this** version: `… | sh -s -- --version {{TAG}}`.

First time on a machine:

```bash
fileswap init --name <name> --server <host>:7341
fileswap service install        # online at every login (or: fileswap start)
```

Step-by-step guide: [docs/install-cli.md](https://github.com/SethSterling22/SwapFile/blob/main/docs/install-cli.md).

<details>
<summary>Manual download of {{TAG}}</summary>

```bash
# Debian / Ubuntu
cd /tmp && wget https://github.com/SethSterling22/SwapFile/releases/download/{{TAG}}/fileswap_amd64.deb && sudo apt install ./fileswap_amd64.deb && rm fileswap_amd64.deb

# Arch Linux
cd /tmp && curl -LO https://github.com/SethSterling22/SwapFile/releases/download/{{TAG}}/fileswap-x86_64.pkg.tar.zst && sudo pacman -U fileswap-x86_64.pkg.tar.zst && rm fileswap-x86_64.pkg.tar.zst

# Any Linux (plain binary)
cd /tmp && curl -LO https://github.com/SethSterling22/SwapFile/releases/download/{{TAG}}/fileswap_linux_amd64.tar.gz && tar xzf fileswap_linux_amd64.tar.gz && sudo install -m 755 fileswap_*_linux_amd64/fileswap /usr/local/bin/ && rm -r fileswap_linux_amd64.tar.gz fileswap_*_linux_amd64
```

On arm64 use `fileswap_arm64.deb`, `fileswap-aarch64.pkg.tar.zst` or `fileswap_linux_arm64.tar.gz`. Check downloads with `sha256sum -c SHA256SUMS --ignore-missing`.

</details>

---

