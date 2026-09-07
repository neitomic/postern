# Onboard a machine

This is the how-to for adding **one computer** (laptop, nuc, pi) to Postern.
The VPS is installed once; each machine is onboarded separately.

If the VPS is not installed yet, do [Install the VPS](#install-the-vps-once) first.

---

## Picture

```
 laptop / nuc / pi          your VPS (public SSH)
  (this machine)            debian@1.2.3.4
        |                         |
        |  autossh -R 127.0.0.1:PORT:127.0.0.1:22
        | ----------------------> |   loopback port booked
        |                         |
 another laptop                   |
  postern ssh macbook  --ProxyJump-->  127.0.0.1:PORT  --> this machine's sshd
```

Three roles:

| Role | What it is | SSH identity |
|---|---|---|
| **VPS** | Debian box with a public IP. Runs `posternd`. | `debian@vps` (admin). Tunnel user `postern` is created for you. |
| **This machine** | The box you want to reach. Behind NAT. | Its own login (`neo@macbook`). Gets a **dedicated tunnel key**. |
| **Laptop** | A computer that can already `ssh debian@vps`. | Same admin key you use for the VPS today. |

The NAT machine must **not** need the VPS admin key. That is why enroll has a two-file path.

---

## What you need on this machine

```bash
# Debian / Ubuntu
sudo apt install autossh openssh-client

# macOS
brew install autossh
```

Install `postern` (linux/darwin, amd64/arm64):

```bash
curl -fsSL https://github.com/neitomic/postern/releases/latest/download/install.sh | sh
```

Or: download the release tarball, then `./postern install`.

Linux: user services die on logout unless lingering is on:

```bash
loginctl enable-linger "$USER"
```

---

## Get a join token

A token is a one-time, 15-minute capability to enroll **one** name. Issue it on the
VPS as root, or from the laptop over admin SSH.

**On the VPS:**

```bash
posternd token issue --ttl 15m --name macbook
```

**From the laptop:**

```bash
ssh -T -o BatchMode=yes debian@YOUR_VPS /usr/bin/posternd token issue --ttl 15m --name macbook
```

It prints **once**:

```
psn_join_<id>.<secret>
```

`--name macbook` binds the token to that hostname. Use the same name in `onboard`.
Paste the token onto this machine (chat, USB, typed). Do not commit it.

`posternd token list` never shows the secret. Expired or used → issue a new one.

---

## Path A — this machine can already `ssh debian@vps`

Use this on a **laptop** that already has admin SSH to the VPS. Do **not** use
`--submit` on a headless nuc/pi (that copies admin SSH onto that box).

```bash
postern onboard \
  --server debian@YOUR_VPS \
  --name macbook \
  --token psn_join_… \
  --submit
```

That, in order:

1. Writes `~/.config/postern/config.toml` and a tunnel key
2. Pins the VPS host key (`ssh-keyscan`)
3. SSHes to the VPS as admin and spends the token
4. Enables the agent (autostart)

Then from any laptop with `postern`:

```bash
postern ls
postern ssh macbook
```

---

## Path B — headless machine (no admin SSH here)

Two computers, **one file each way**. The VPS is only talked to from the laptop.

**1. This machine** (nuc / pi):

```bash
postern onboard --server debian@YOUR_VPS --name nuc --token psn_join_…
```

It writes `~/.local/share/postern/enroll-request.json` and prints `scp` lines
with this machine’s user and hostname.

**2. Laptop** (already `ssh debian@YOUR_VPS`):

```bash
# pull the request (use the scp line onboard printed)
scp USER@NUC:.local/share/postern/enroll-request.json .

# laptop talks to the VPS — this spends the token
postern enroll-machine enroll-request.json > enroll-response.json

# send the answer back
scp enroll-response.json USER@NUC:
```

The laptop needs `postern config set server debian@YOUR_VPS` once (same as
`onboard --server` on the laptop).

**3. This machine** again:

```bash
postern onboard --apply-response enroll-response.json
```

You should see `onboarded nuc  port 22xx`. The agent picks up the tunnel
within a few seconds.

```
this machine                         laptop                         VPS
     |                                  |                            |
     |  enroll-request.json  ---------> |                            |
     |                                  |  posternd enroll --json -> |
     |                                  |  <- { ok, port, … }        |
     |  <----- enroll-response.json     |                            |
     |  save state, agent starts        |                            |
```

---

## What “server” and `vps_hostname` are

| Setting | Where | Used by |
|---|---|---|
| `--server debian@HOST` | this machine’s `config.toml` | **Your laptop** and **onboard --submit**: admin SSH and ProxyJump |
| `vps_hostname` in `/etc/postern/posternd.toml` | VPS | **The agent** (autossh `HostName`) |

They must be the **same VPS**. Prefer the public DNS or public IP you actually
SSH to, not `hostname -f` (`lin-hcm` is usually wrong).

```bash
# on the VPS
edit /etc/postern/posternd.toml    # vps_hostname = "203.0.113.10"
systemctl restart posternd
```

Machines already enrolled keep the old value in `state.json` until you onboard
again with a new token.

---

## Verify

**On the VPS:**

```bash
ss -ltn src 127.0.0.1 | grep -E '22[0-9][0-9]'
posternd hosts list
```

**On a laptop** (after `postern onboard --server debian@YOUR_VPS` once):

```bash
postern ls          # TUNNEL + AGENT should be up / online
postern ssh macbook
```

`postern ssh` uses a generated `-F` config (jump + host). It does not pass
inline `ProxyJump=user@host`.

Optional: `postern ssh-config --write` updates `~/.ssh/config` (managed block
only). Backup that file first.

---

## If it fails

| What you see | What it means |
|---|---|
| `invalid join token` / `token_expired` | Issue a new token (15 min, single use) |
| `name_collision` | That name exists with a different key. `posternd hosts rekey` or `hosts rm`, then a new token |
| `admin SSH to … failed` on `--submit` | This machine cannot `ssh debian@vps`. Use Path B |
| enroll-machine hangs / permission denied | Laptop’s `server` is wrong, or you are not in group `postern` (open a **new** SSH session after bootstrap) |
| `ssh macbook` hangs | Tunnel down. `postern ls`, `postern agent status`, VPS `ss -ltn src 127.0.0.1` |
| Agent dies on Linux logout | `loginctl enable-linger $USER` |
| `install autossh` | `apt install autossh` or `brew install autossh` |
| Key login asks for a password | sshd StrictModes: home dir must be owned by that user (`chown root:root /root` if `/root` is uid 1001) |
| Tunnel HostName is `lin-hcm` | Set `vps_hostname` to the public IP/DNS, restart posternd, re-enroll |

---

## Install the VPS (once)

Debian 11+, OpenSSH 8.0+. Use the **linux** tarball. Do not install the
`postern` client as the VPS tunnel.

```bash
tar -xzf postern_*_linux_*.tar.gz
# as root
POSTERN_VPS_HOSTNAME=YOUR.PUBLIC.DNS.OR.IP POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh
# or create an admin login if the image is root-only:
POSTERN_VPS_HOSTNAME=… POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh --create debian
```

Then a **new** SSH session as that admin so group `postern` is visible:

```bash
ssh debian@YOUR_VPS
posternd token issue --ttl 15m --name macbook
```

More detail (sshd Match, lockout recovery): [README.md](README.md).

---

## Commands (short)

```
# this machine
postern onboard --server debian@VPS --name NAME --token TOKEN [--submit]
postern onboard --apply-response enroll-response.json
postern ls
postern ssh NAME
postern agent status

# VPS / laptop admin
posternd token issue --ttl 15m --name NAME
postern enroll-machine enroll-request.json
```
