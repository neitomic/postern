# Postern

Control plane for reverse SSH tunnels. A Debian VPS with a static IP books
loopback ports; machines behind NAT keep an `autossh -R 127.0.0.1:PORT:127.0.0.1:22`
up; laptops `ProxyJump` through the VPS onto that port.

Postern is **not a VPN**. No tun device, no default-route grab, no fight with
Tailscale / WireGuard / a corporate VPN on the same box. Encryption stays SSH.
The VPS is a TCP relay, not a TLS terminator. `posternd` listens on a **Unix
socket only** — no public HTTP, no loopback TCP API, no web UI.

Two binaries, one module:

| Binary | Where | Job |
|---|---|---|
| `posternd` | VPS | registry, port allocator, join tokens, `authorized_keys`, heartbeat |
| `postern` | each machine / laptop | join, agent (autossh), `ls` / `ssh-config` / `ssh` |

Full spec: [DESIGN.md](DESIGN.md).

## Build

Go 1.25+, `CGO_ENABLED=0` (pure-Go SQLite).

```bash
make test
make build          # native dist/posternd dist/postern
make dist           # linux/amd64, darwin/arm64, darwin/amd64 into dist/<goos>-<goarch>/
```

## Install

### VPS (Debian 11+, OpenSSH 8.0+)

Copy the linux/amd64 `posternd` binary plus `scripts/` and `contrib/` onto the
VPS (or clone the repo there). Do **not** install the `postern` client on the
VPS.

```bash
# from this repo on the VPS, as root
POSTERND_BIN=dist/linux-amd64/posternd ./scripts/vps-bootstrap.sh debian
```

`debian` is the admin SSH user (or `$SUDO_USER` / the first non-root argument).
The script:

- `groupadd --system postern` and `useradd --system` with shell `/usr/bin/posternd-shell`
- `usermod -aG postern` that admin user
- `/var/lib/postern` and `/etc/postern` `0750`; `authorized_keys` and `postern.db` `0640`
- installs `contrib/sshd/50-postern.conf` (ends with `Match all`) and **runs `sshd -t`**
  — **hard-fail, no `systemctl reload ssh`** if the config is invalid
- installs `contrib/systemd/posternd.service`, `systemctl enable --now posternd`
- installs `/usr/bin/posternd` and a `posternd-shell` symlink
- does **not** raise `MaxStartups`

Then open a **new** SSH session as the admin user so `SO_PEERGROUPS` sees group
`postern`. Edit `/etc/postern/posternd.toml` `vps_hostname` if the guessed DNS
name is wrong and `systemctl restart posternd`.

### Agent (NAT machine)

Need `autossh` and OpenSSH on the path:

```bash
# Debian / Ubuntu
sudo apt install autossh openssh-client

# macOS
brew install autossh
```

Install `dist/<goos>-<goarch>/postern` onto `$PATH` (for example
`~/.local/bin/postern`). Split-enroll is the canary — see below — then:

```bash
postern agent install
postern agent enable
```

Linux user units die on logout unless lingering is on:

```bash
loginctl enable-linger "$USER"
```

`postern agent enable` warns if linger is `no`.

### Client (operator laptop)

Same `postern` binary. After at least one host is enrolled:

```bash
postern init --server debian@vps.example.net --accept-host-key
postern ls
postern ssh-config --write    # backup ~/.ssh/config first; replaces the managed block only
postern ssh macbook
```

`postern ssh` execs `/usr/bin/ssh -F` a generated config (jump + host stanzas).
It does not pass inline `ProxyJump=user@host`.

## Canary: split enroll (not `--submit`)

The NAT machine never needs the admin SSH key. Token + two JSON files is the
path that works on a headless nuc/pi.

**1. Operator laptop** (already `debian@vps`):

```bash
postern init --server debian@vps.example.net --accept-host-key
ssh -T -o BatchMode=yes debian@vps.example.net /usr/bin/posternd token issue --ttl 15m --name macbook
```

Paste the `psn_join_…` token onto the machine (chat / USB / typed). Bound
`--name` is recommended.

**2. Canary machine** (no admin SSH):

```bash
postern init --server debian@vps.example.net --name macbook --login-user neo --accept-host-key
postern join --token psn_join_…
# writes ~/.local/share/postern/enroll-request.json and prints it
```

Copy that JSON to the laptop.

**3. Laptop** submits enroll over **its** admin SSH:

```bash
postern enroll-machine enroll-request.json > enroll-response.json
```

Copy the response back to the machine.

**4. Machine** binds the response, then starts the tunnel:

```bash
postern join --apply-response enroll-response.json
postern agent install && postern agent enable
```

`--apply-response` refuses to write `state.json` unless `ok`, name, fingerprint,
and port range match.

Optional convenience, **not** the headless path:

```bash
postern join --submit --token psn_join_…
```

That SSHes to the VPS **as the admin user from the machine**, which copies admin
authority onto that box. Do not treat it as the only enroll path. Never
ssh-agent forwarding (`-A`).

Verify: `ss -ltn src 127.0.0.1` on the VPS for the allocated port, `postern ls`
(TUNNEL + AGENT columns), `postern ssh macbook`.

## CLI cheat sheet

`postern` (machine / laptop):

```
postern init [--server USER@HOST] [--name NAME] [--login-user USER] [--local-ssh-port 22] [--accept-host-key]
postern join --token TOKEN [--force]          # write enroll-request.json; do not SSH as admin
postern join --apply-response FILE            # bind name/fp/port; write state.json + ssh_config
postern join --submit --token TOKEN [--force] # optional; copies admin SSH onto this machine
postern enroll-machine FILE                   # laptop: ssh posternd enroll --json
postern agent install | uninstall | enable | disable | status
postern agent run                             # foreground; supervisor exec
postern agent set-name NAME
postern ls [--json]
postern ssh-config [--write] [--path PATH]
postern ssh <name> [-- ssh-args...]
postern version
```

`posternd` (VPS; also `ssh debian@vps /usr/bin/posternd …`):

```
posternd serve [--config /etc/postern/posternd.toml]
posternd token issue [--ttl 15m] [--name HOST] [--note TEXT] [--json]
posternd token list [--json]
posternd token revoke <id>
posternd enroll [--json]
posternd hosts list [--json]
posternd hosts show <name> [--json]
posternd hosts rm <name> [--kill-listen]
posternd hosts disable | enable <name>
posternd hosts rename <old> <new>
posternd hosts rekey <name> --old-fingerprint SHA256:… [--json]
posternd gc
posternd ports audit
posternd authorized-keys render
posternd version
```

## License

MIT. See [LICENSE](LICENSE).
