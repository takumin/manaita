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
manaita lock                              # write the hashes of the plugins to manaita.lock
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

# Inventory files, from the least to the most specific (the default, with *.yaml after each *.yml).
# {hostname} and {domain} come from the host; a pattern is skipped when one is empty.
hosts:
  - hosts/all/*.yml
  - hosts/domains/{domain}/*.yml
  - hosts/hosts/{hostname}/*.yml
  - hosts/fqdns/{domain}/{hostname}/*.yml

# Node attribute files, in the same way.
nodes:
  - nodes/all/*.yml
  - nodes/domains/{domain}/*.yml
  - nodes/hosts/{hostname}/*.yml
  - nodes/fqdns/{domain}/{hostname}/*.yml

prelude: # recipes run before every run list
  - helpers/keeper.rb

plugins: # mitamae plugins, pinned to a commit (see Plugins)
  - repo: github.com/takumin/itamae-plugin-recipe-apt
    rev: 3f2a6c0e9b1d4a7f8e5c2b0a9d6f3e1c7b4a8d2e

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

The patterns of `hosts` can lay the inventory out differently,
like the run lists next to the node files of the same layer, or a file per host:

```yaml
hosts:
  - nodes/all/recipes/*.yml
  - nodes/domains/{domain}/recipes/*.yml
  - nodes/hosts/{hostname}/recipes/*.yml
  - inventory/{hostname}.{domain}.yml
```

`manaita apply` asks the host for its short hostname (`hostname -s`) and its domain (`dnsdomainname`) before copying anything,
and picks the layers with them, so a hostname can have a different run list in each domain.
The host must be declared by a pattern holding `{hostname}` that matches at least one file; `{hostname}` never matches a dot, the rest of the FQDN being the domain.
The files are merged in the order of the patterns and, within a pattern, in the order of their names, each file only once:
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

`manaita list` names the hosts by their FQDN, or their hostname alone for a pattern without `{domain}`, like `hosts/{hostname}/`, which applies in any domain.
`manaita show` takes such a name, without connecting, and lists the inventory files merged into the host.
`manaita plan` takes the same arguments and flags as `apply`, and runs mitamae with `--dry-run`.
Without a name, `show`, `plan` and `apply` identify this machine by its hostname and domain, and apply runs mitamae from the project in place.
Applying to this machine needs no terminal, so it can run unattended from cloud-init or a systemd unit once the hostname is set.
On this machine, sudo preserves the proxy variables set in the environment (`http_proxy`, `https_proxy`, `ftp_proxy`, `all_proxy`, `no_proxy` and their uppercase forms) for mitamae; on a remote host, mitamae sees the environment of that host.
A node file matched by several patterns, directly or through a symlink, is passed only once.
A `mitamae` found in `PATH` is used when it matches the checksum of the arch, so an image with mitamae installed applies without the network.
Otherwise the binaries are downloaded from the mitamae releases into the cache directory, possibly through a [cache server](#cache-servers), and verified against their checksums.
The cache directory is `--cache-dir` when set, else `manaita` under the user cache directory (`$XDG_CACHE_HOME` or `~/.cache`),
else `/var/lib/cache/manaita` when neither `HOME` nor `XDG_CACHE_HOME` is set.
The remote user must be able to run `sudo`; with a single host and a terminal, sudo can prompt for a password.

## Plugins

`plugins` lists mitamae plugins by their GitHub repository and the full SHA of a commit, so that the recipes need not live in the project.
The name of the repository must start with `itamae-plugin-recipe-` or `itamae-plugin-resource-` (or their `mitamae-` forms), as mitamae looks them up by it.
A plugin depending on another is not resolved: list every plugin.

The plugins are downloaded from `https://codeload.github.com/<owner>/<repo>/tar.gz/<rev>`, possibly through a [cache server](#cache-servers),
extracted into `plugins/<repo>/<rev>` of the cache directory, and verified against their hashes in `manaita.lock`, next to `manaita.yml`.
The hash is computed from the extracted files like the `h1:` hashes of `go.sum`, since the archives of GitHub may change for the same commit.
An archive holding anything but directories, regular files and symlinks is refused, and the file modes are not part of the hash.
A symlink must lead inside the plugin, without a loop; it is hashed, and copied to the remote hosts, as what it leads to.

```
github.com/takumin/itamae-plugin-recipe-apt 3f2a6c0e9b1d4a7f8e5c2b0a9d6f3e1c7b4a8d2e h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=
```

`manaita lock` downloads every plugin from its origin, never through the proxy list since the hashes are what the cache servers are checked against,
and rewrites `manaita.lock` with their hashes.
Run it after changing `plugins`: `apply` and `plan` refuse a plugin missing from `manaita.lock`.
When a bot such as Renovate updates the commits, a CI job running `manaita lock` and pushing the result, like [autofix.ci](https://autofix.ci), keeps the lock file in step.

A run list names a recipe of a plugin as `include_recipe` does: `apt` runs its `default.rb`, and `apt::source` its `source.rb`.
A file of the project with the same name comes first.
manaita gives mitamae a recipe including each of them, and, when `plugins` is not empty, a plugin directory holding the plugins of `plugins` and those of the `plugins` directory of the project, which must not share a name.
For a remote host, the plugins are copied to `.manaita/plugins` of the remote project with `rsync`; on this machine, they are linked from `stage` of the cache directory.

## Configuration

Each flag can also be set by an environment variable, or by the configuration file of the machine running manaita,
in this order of precedence: the command line, the environment, then the file.
The file is `/etc/manaita/config.yml` (ignored when missing), else `--config` or `$MANAITA_CONFIG`; an empty path reads no file.
It maps the names of the flags, with underscores instead of hyphens, to their values,
so it can be written by cloud-init (`write_files`) or Ignition (`storage.files`) when the machine boots:

```yaml
# /etc/manaita/config.yml
chdir: /srv/mitamae
cache_dir: /var/cache/manaita
proxy: http://cache.internal|direct
```

| Flag                        | Environment         | Key                 |
| --------------------------- | ------------------- | ------------------- |
| `--log-level`, `-l`         | `LOG_LEVEL`         | `log_level`         |
| `--log-format`, `-f`        | `LOG_FORMAT`        | `log_format`        |
| `--chdir`, `-C`             | `MANAITA_CHDIR`     | `chdir`             |
| `--cache-dir`               | `MANAITA_CACHE_DIR` | `cache_dir`         |
| `--proxy`                   | `MANAITA_PROXY`     | `proxy`             |
| `--mitamae-log-level`, `-L` |                     | `mitamae_log_level` |
| `--parallel`, `-j`          |                     | `parallel`          |

An unknown key in the file is an error, so that a misspelled key is not silently ignored.

## Cache servers

The downloads, such as the mitamae binaries, keep their origin in `manaita.yml`, and the machine running manaita decides where they really come from,
like `url.<base>.insteadOf` for git or `GOPROXY` for Go.
So many machines booting at once, or a network slow to reach the internet, can download from a cache server without changing the project.

`--proxy` takes a list with the syntax and the meaning of `GOPROXY`: cache server URLs, `direct` for the origin and `off` to disallow downloading.
After a comma, the next element is tried only when the one before answers 404 or 410;
after a pipe, it is tried on any error, such as an unreachable server, a 5xx answer or a checksum mismatch.
The default is `direct`.

```sh
manaita --proxy 'http://cache.internal|direct' apply  # the origin when the cache server fails
manaita --proxy 'http://cache.internal,direct' apply  # the origin only for the URLs the cache server does not serve
manaita --proxy 'http://cache.internal' apply         # the cache server only
```

A cache server serves the origin `https://host/path` at `<cache server URL>/host/path`; only https origins without a port go through it.
The downloads are verified against the checksums of `manaita.yml`, so the cache server is not trusted and plain http is enough.
The proxy variables (`HTTP_PROXY` and the others) still apply to the requests; list the cache server in `NO_PROXY` if it must be reached directly.

A reverse proxy caching by the path is enough.
The URLs hold a version, so their content does not change and the cache can be kept for as long as the disk allows.
GitHub redirects the release downloads to short-lived signed URLs, so the cache server follows the redirects itself and caches the result under the original URL:

```nginx
# /etc/nginx/conf.d/manaita.conf
proxy_cache_path /var/cache/manaita keys_zone=manaita:10m max_size=20g inactive=365d use_temp_path=off;

server {
  listen 80;
  server_name cache.internal;
  resolver 127.0.0.53; # proxy_pass to the redirects resolves their hosts at run time

  proxy_ssl_server_name on;
  proxy_cache manaita;
  proxy_cache_key $request_uri; # the original URL, also for the redirects followed
  proxy_cache_valid 200 365d;
  proxy_cache_lock on; # a single request upstream for concurrent ones
  proxy_buffer_size 16k; # the signed URLs of the redirects are long
  proxy_buffers 8 16k;
  proxy_busy_buffers_size 32k;
  add_header X-Cache-Status $upstream_cache_status always;

  location /github.com/ {
    proxy_pass https://github.com/;
    proxy_intercept_errors on;
    error_page 301 302 303 307 308 = @redirect;
  }

  location /codeload.github.com/ {
    proxy_pass https://codeload.github.com/;
  }

  location @redirect {
    set $location $upstream_http_location;
    proxy_pass $location;
  }
}
```

When a cache server answers with a redirect to another host instead, manaita follows it with a warning that the download is not cached.
