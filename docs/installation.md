# Installation

## Quick install (curl)

```shell
curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | sh
```

Downloads the latest release binary for your OS and architecture, verifies its SHA-256 against
the published `checksums.txt`, and installs it to `~/.local/bin` - no root or sudo needed. Only
`curl` and `tar` are required.

| Environment | Description |
|-------------|-------------|
| `PREFIX`    | Install directory (default `~/.local/bin`) |
| `VERSION`   | Release tag to install, e.g. `v0.2.0` (default: latest) |

```shell
curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | PREFIX=/usr/local/bin sh
curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | VERSION=v0.2.0 sh
```

If `~/.local/bin` is not on your `PATH`, the installer prints the line to add to your shell
profile. Piping a script into a shell runs whatever the script contains - inspect
[install.sh](https://github.com/soroushalinia/backupd/blob/main/install.sh) first if you prefer.

## Prebuilt binaries

Download the latest release for your platform from the
[releases page](https://github.com/soroushalinia/backupd/releases) (built with GoReleaser).

## From source

```shell
git clone https://github.com/soroushalinia/backupd.git
cd backupd
make build
```

## Go install

```shell
go install github.com/soroushalinia/backupd/cmd/backupd@latest
```

## Shell completion

```shell
backupd completion bash > /etc/bash_completion.d/backupd   # bash
backupd completion zsh  > "${fpath[1]}/_backupd"            # zsh
backupd completion fish > ~/.config/fish/completions/backupd.fish  # fish
```

## Requirements

- Go 1.24+ to build from source
- No runtime dependencies - backupd is a single static binary
