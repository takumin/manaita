# manaita

Deploy mitamae recipes to hosts over ssh

[![CI](https://github.com/takumin/manaita/actions/workflows/ci.yml/badge.svg)](https://github.com/takumin/manaita/actions/workflows/ci.yml)
[![Coverage](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/manaita/coverage.svg)](https://github.com/takumin/manaita/actions/workflows/ci.yml)
[![Code to Test Ratio](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/manaita/ratio.svg)](https://github.com/takumin/manaita/actions/workflows/ci.yml)
[![Test Execution Time](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/manaita/time.svg)](https://github.com/takumin/manaita/actions/workflows/ci.yml)

## Overview

manaita applies the recipes of a [mitamae](https://github.com/itamae-kitchen/mitamae) repository to hosts.
Each host of the inventory declares its run list, so what a host gets lives in the repository instead of a command line.

For a remote host, manaita copies the repository and the pinned mitamae binary with `rsync`, then runs `mitamae local` over `ssh`.
`ssh` and `rsync` are the external commands, so `~/.ssh/config`, the agent and `ProxyJump` apply as usual.
The node attribute files are passed to mitamae as they are, so they merge exactly like with `mitamae local -y`.

## Installation

Download the binary for your platform from the [releases](https://github.com/takumin/manaita/releases) and verify it:

```sh
VERSION=v0.2.0
BIN="manaita_${VERSION}_linux_amd64"
BASE="https://github.com/takumin/manaita/releases/download/${VERSION}"
curl -fsSLO "${BASE}/${BIN}" -O "${BASE}/${BIN}.sig" -O "${BASE}/${BIN}.cert" -O "${BASE}/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
cosign verify-blob "${BIN}" \
  --signature "${BIN}.sig" \
  --certificate "${BIN}.cert" \
  --certificate-identity-regexp '^https://github\.com/takumin/manaita/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify "${BIN}" --repo takumin/manaita # alternatively
install -m 0755 "${BIN}" ~/.local/bin/manaita
```

Or build it with Go:

```sh
go install github.com/takumin/manaita@latest
```

## Usage

```sh
manaita list                              # hosts of the inventory
manaita show                              # node files, recipes and command of this machine
manaita show dsk.metal.internal           # node files, recipes and command of a host
manaita plan                              # changes apply would make to this machine (mitamae dry run)
manaita plan dsk                          # changes apply would make to a host
manaita apply                             # apply to this machine
manaita apply dsk                         # apply the run list over ssh (any ssh destination)
manaita apply -L debug dsk                # mitamae debug log
manaita apply -r cookbooks/server/nginx dsk  # apply a recipe instead of the run list
manaita apply -j 4 rpi4-8g-{1..4}         # several hosts in parallel
```

`-C DIR` selects the project; by default it is found by walking up from the current directory.

## Project

The root of a project holds `manaita.yml`:

```yaml
mitamae:
  version: 2.0.3
  checksums: # SHA-256 of the release binaries, by arch
    x86_64: 61c6b2a678f45c1374506874846cf362bc418e4914dd0cf5eb56351210dcb930
    aarch64: 60896c5598eab03283e6f1df95fea861217c69f7a259eb6cca097c1dca34c46b

hosts: hosts # inventory directory (default); {layer} places the layers inside it, like nodes/{layer}/recipes

# Node attribute files, from the least to the most specific.
# {hostname} and {domain} come from the host; a pattern is skipped when one is empty.
nodes:
  - nodes/all/*.yml
  - nodes/domains/{domain}/*.yml
  - nodes/hosts/{hostname}/*.yml
  - nodes/fqdns/{domain}/{hostname}/*.yml

prelude: # recipes run before every run list
  - helpers/keeper.rb

remote:
  path: mitamae # destination, relative to the home directory (default)
  exclude: # rsync patterns not copied
    - /.git/
```

The inventory is layered like the node files, from the least to the most specific:

```
hosts/
├── all/*.yml                          # every host
├── domains/{domain}/*.yml             # the hosts of a domain
├── hosts/{hostname}/*.yml             # a host, whatever its domain
└── fqdns/{domain}/{hostname}/*.yml    # a host of a domain
```

When `hosts` holds `{layer}`, it is replaced by the path of each layer instead,
so that the run lists can live next to the node files of the same layer:

```
nodes/
├── all/recipes/*.yml
├── domains/{domain}/recipes/*.yml
├── hosts/{hostname}/recipes/*.yml
└── fqdns/{domain}/{hostname}/recipes/*.yml
```

`manaita apply` asks the host for its short hostname (`hostname -s`) and its domain (`dnsdomainname`) before copying anything,
and picks the layers with them, so a hostname can have a different run list in each domain.
The host must be declared by a directory of `hosts/` or `fqdns/{domain}/` holding at least one file.
The files are merged layer by layer and, within a layer, in the order of their names:
the run lists are concatenated, skipping the recipes already listed.

```yaml
# hosts/all/common.yml
run_list:
  - cookbooks/common/sudo # a directory runs its default.rb

# hosts/domains/metal.internal/common.yml
run_list:
  - cookbooks/common/systemd

# hosts/fqdns/metal.internal/rpi4-8g-1/host.yml
run_list:
  - cookbooks/server/knot-resolver
  - roles/rpi4-8g.rb
```

`manaita list` names the hosts by their FQDN, or their hostname alone for `hosts/{hostname}/` which applies in any domain.
`manaita show` takes such a name, without connecting, and lists the inventory files merged into the host.
`manaita plan` takes the same arguments and flags as `apply`, and runs mitamae with `--dry-run`.
Without a name, `show`, `plan` and `apply` identify this machine by its hostname and domain, and apply runs mitamae from the project in place.
Applying to this machine needs no terminal, so it can run unattended from cloud-init or a systemd unit once the hostname is set.
On this machine, sudo preserves the proxy variables set in the environment (`http_proxy`, `https_proxy`, `ftp_proxy`, `all_proxy`, `no_proxy` and their uppercase forms) for mitamae; on a remote host, mitamae sees the environment of that host.
A node file matched by several patterns, directly or through a symlink, is passed only once.
A `mitamae` found in `PATH` is used when it matches the checksum of the arch, so an image with mitamae installed applies without the network.
Otherwise the binaries are downloaded from the mitamae releases into the cache directory and verified against their checksums.
The cache directory is `$MANAITA_CACHE_DIR` when set, else `manaita` under the user cache directory (`$XDG_CACHE_HOME` or `~/.cache`),
else `/var/lib/cache/manaita` when neither `HOME` nor `XDG_CACHE_HOME` is set.
The remote user must be able to run `sudo`; with a single host and a terminal, sudo can prompt for a password.
