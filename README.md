# manaita

Deploy mitamae recipes to hosts over ssh

[![CI](https://github.com/takumin/manaita/actions/workflows/integration.yml/badge.svg)](https://github.com/takumin/manaita/actions/workflows/integration.yml)
[![Coverage](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/manaita/coverage.svg)](https://github.com/takumin/manaita/actions/workflows/integration.yml)
[![Code to Test Ratio](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/manaita/ratio.svg)](https://github.com/takumin/manaita/actions/workflows/integration.yml)
[![Test Execution Time](https://raw.githubusercontent.com/takumin/octocov-central/main/badges/takumin/manaita/time.svg)](https://github.com/takumin/manaita/actions/workflows/integration.yml)

## Overview

manaita applies the recipes of a [mitamae](https://github.com/itamae-kitchen/mitamae) repository to hosts.
Each host of the inventory declares its run list, so what a host gets lives in the repository instead of a command line.

For a remote host, manaita copies the repository and the pinned mitamae binary with `rsync`, then runs `mitamae local` over `ssh`.
`ssh` and `rsync` are the external commands, so `~/.ssh/config`, the agent and `ProxyJump` apply as usual.
The node attribute files are passed to mitamae as they are, so they merge exactly like with `mitamae local -y`.

## Usage

```sh
manaita list                              # hosts of the inventory
manaita show dsk                          # node files, recipes and command of a host
manaita apply dsk                         # apply the run list over ssh
manaita apply -n dsk                      # dry run
manaita apply -L debug dsk                # mitamae debug log
manaita apply -r cookbooks/server/nginx dsk  # apply a recipe instead of the run list
manaita apply -j 4 rpi4-8g-{1..4}         # several hosts in parallel
manaita apply --local                     # apply to this machine, named by its short hostname
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

hosts: hosts # inventory directory (default)

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

Each host is a file of the inventory, named after the host:

```yaml
# hosts/rpi4-8g-1.yml
ssh: rpi4-8g-1.metal.internal # ssh destination (default: the file name)
hostname: rpi4-8g-1 # used in the node patterns (default: the file name)
domain: metal.internal # used in the node patterns
run_list:
  - cookbooks/common/sudo # a directory runs its default.rb
  - cookbooks/server/knot-resolver
  - roles/rpi4-8g.rb
```

A node file matched by several patterns, directly or through a symlink, is passed only once.
The binaries are downloaded from the mitamae releases into the user cache directory and verified against their checksums.
The remote user must be able to run `sudo`; with a single host and a terminal, sudo can prompt for a password.
