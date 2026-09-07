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

Full spec: [DESIGN.md](DESIGN.md). **Adding a machine:** [ONBOARDING.md](ONBOARDING.md).

```bash
# this machine can already ssh debian@vps:
postern onboard --server debian@YOUR_VPS --name macbook --token psn_join_… --submit

# headless (no admin SSH on this box):
postern onboard --server debian@YOUR_VPS --name nuc --token psn_join_…
# laptop: enroll-machine the printed file, scp the response back, then:
postern onboard --apply-response enroll-response.json
```

Issue the token on the VPS: `posternd token issue --ttl 15m --name macbook`.

## Install the VPS (`posternd`)

Debian 11+, OpenSSH 8.0+. Use the **linux** release tarball (amd64 or arm64).
Do **not** install the `postern` client as the VPS tunnel.

```bash
tar -xzf postern_*_linux_*.tar.gz
# as root, from the extracted directory
POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh
# or pass the login you SSH as:
POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh ubuntu
# root-only image (creates the login, copies /root/.ssh/authorized_keys):
POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh --create debian
```

The admin user is the login you SSH as (added to group `postern`). It must not
be `root` or `postern`. With no argument the script uses `$SUDO_USER`, then
`debian` / `ubuntu` if they exist, then the first uid≥1000 login. `root` can
already talk to the Unix socket (uid 0 is an API admin); you still want a
normal SSH login for `postern config set server USER@HOST`.

The script:

- `groupadd --system postern` and `useradd --system` with shell `/usr/bin/posternd-shell`
- `usermod -aG postern` that admin user
- `/var/lib/postern` and `/etc/postern` `0750`; `authorized_keys` and `postern.db` `0640`
- installs `PermitUserEnvironment` in `sshd_config.d` and appends `Match User postern`
  at the **end** of `/etc/ssh/sshd_config` (Debian `Include` is at the top; a Match
  drop-in would wrap `UsePAM` / `PermitRootLogin` and can lock out root)
- **runs `sshd -t`** and `sshd -T` for `root`, the admin user, and `postern`
  — **hard-fail, no `systemctl reload ssh`** if Match leaked or the config is invalid
- installs `contrib/systemd/posternd.service`, `systemctl enable --now posternd`
- installs `/usr/bin/posternd` and a `posternd-shell` symlink
- does **not** raise `MaxStartups`

Then open a **new** SSH session as the admin user so `SO_PEERGROUPS` sees group
`postern`. Edit `/etc/postern/posternd.toml` `vps_hostname` if the guessed DNS
name is wrong and `systemctl restart posternd`.

If SSH as `root` dies after bootstrap: use the provider console (or the admin
login created with `--create`, same key as root) and:

```bash
rm -f /etc/ssh/sshd_config.d/50-postern.conf
sed -i '/^# BEGIN POSTERN MATCH$/,/^# END POSTERN MATCH$/d' /etc/ssh/sshd_config
sshd -t && systemctl restart ssh
```

`posternd.service` does not change sshd. The drop-in / Match block does. After
recovery, `ssh debian@vps` (or whoever you passed to bootstrap) is the intended
admin path; `PermitRootLogin no` from cloud-init can also appear on the first
sshd reload.

## Client (operator laptop)

Same `postern` binary. After at least one host is enrolled:

```bash
postern ls
postern ssh-config --write    # backup ~/.ssh/config first; replaces the managed block only
postern ssh macbook
```

`postern ssh` execs `/usr/bin/ssh -F` a generated config (jump + host stanzas).
It does not pass inline `ProxyJump=user@host`.

## Build

Go 1.25+, `CGO_ENABLED=0` (pure-Go SQLite).

```bash
make test
make build          # native dist/posternd dist/postern
make dist           # linux/amd64, linux/arm64, darwin/arm64, darwin/amd64
make pack VERSION=0.1.0
```

GitHub Actions publishes those tarballs on `v*` tags.

## CLI cheat sheet

`postern` (machine / laptop):

```
postern onboard --server USER@HOST --name NAME --token TOKEN [--submit]
postern onboard --apply-response FILE
postern install [--bin-dir DIR] [--no-enable] [--no-service]
postern uninstall
postern config
postern config show
postern config get KEY
postern config set KEY VALUE [--accept-host-key]
postern config accept-host-key
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
