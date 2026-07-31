# Installation

## Quick install (curl)

```shell
curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | sh
```

| Environment | Description |
|-------------|-------------|
| `PREFIX`    | Install directory (default `~/.local/bin`) |
| `VERSION`   | Release tag to install (default: latest) |

### Pinning a version

```shell
curl -fsSL https://raw.githubusercontent.com/soroushalinia/backupd/main/install.sh | VERSION=v0.2.0 sh
```

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
