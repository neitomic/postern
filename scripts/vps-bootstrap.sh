#!/bin/sh
# Install posternd on a Debian VPS: tunnel user, dirs, sshd drop-in, systemd.
# posternd listens on a Unix socket only. Do not add a TCP bind.
#
# Usage (as root):
#   POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh [admin-user]
# admin-user defaults to $SUDO_USER, then debian. Must not be root or postern.
#
# sshd -t is a hard-fail: this script never reloads ssh if the config is invalid.
# Do not set MaxStartups in the Postern drop-in (it is global and can lock out the admin).

set -eu

die() {
	echo "vps-bootstrap: $*" >&2
	exit 1
}

if [ "$(uname -s)" != "Linux" ]; then
	die "this script is for the Debian VPS (Linux)"
fi
if [ "$(id -u)" -ne 0 ]; then
	die "run as root (sudo $0 [admin-user])"
fi

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
SSHD_FIXTURE="$ROOT/contrib/sshd/50-postern.conf"
UNIT_FIXTURE="$ROOT/contrib/systemd/posternd.service"
SSHD_DROPIN=/etc/ssh/sshd_config.d/50-postern.conf
UNIT_DEST=/etc/systemd/system/posternd.service
CONF_DEST=/etc/postern/posternd.toml

ADMIN=""
if [ "${1-}" != "" ] && [ "$1" != "root" ]; then
	ADMIN=$1
elif [ "${SUDO_USER-}" != "" ] && [ "$SUDO_USER" != "root" ]; then
	ADMIN=$SUDO_USER
else
	ADMIN=debian
fi
if [ "$ADMIN" = "postern" ]; then
	die "admin user must not be postern (uid postern is never admin)"
fi
getent passwd "$ADMIN" >/dev/null || die "admin user $ADMIN does not exist"

[ -f "$SSHD_FIXTURE" ] || die "missing $SSHD_FIXTURE"
[ -f "$UNIT_FIXTURE" ] || die "missing $UNIT_FIXTURE"
if ! grep -q '^Match all$' "$SSHD_FIXTURE"; then
	die "$SSHD_FIXTURE must contain 'Match all' so Debian Include does not capture the rest of sshd_config"
fi
if grep -qi '^[[:space:]]*MaxStartups' "$SSHD_FIXTURE"; then
	die "do not raise MaxStartups in the Postern drop-in"
fi

BIN=""
if [ "${POSTERND_BIN-}" != "" ]; then
	BIN=$POSTERND_BIN
else
	for c in "$ROOT/dist/linux-amd64/posternd" "$ROOT/dist/posternd" ./posternd /usr/bin/posternd; do
		if [ -x "$c" ]; then
			BIN=$c
			break
		fi
	done
fi
[ -n "$BIN" ] && [ -x "$BIN" ] || die "posternd binary not found (set POSTERND_BIN or make dist)"

SSHD=/usr/sbin/sshd
if [ ! -x "$SSHD" ]; then
	SSHD=$(command -v sshd) || die "sshd not found"
fi

echo "vps-bootstrap: admin=$ADMIN bin=$BIN"

install -o root -g root -m 0755 "$BIN" /usr/bin/posternd
ln -sfn posternd /usr/bin/posternd-shell
# login_shell must be listed or some PAM stacks reject the tunnel user
if [ -f /etc/shells ] && ! grep -Fqx /usr/bin/posternd-shell /etc/shells; then
	echo /usr/bin/posternd-shell >>/etc/shells
fi

if ! getent group postern >/dev/null; then
	groupadd --system postern
fi
if ! getent passwd postern >/dev/null; then
	useradd --system --home /var/lib/postern --shell /usr/bin/posternd-shell \
		--gid postern --comment "Postern tunnel" postern
else
	usermod --home /var/lib/postern --shell /usr/bin/posternd-shell --gid postern postern
fi
usermod -aG postern "$ADMIN"

install -d -o postern -g postern -m 0750 /var/lib/postern
install -d -o root -g postern -m 0750 /etc/postern
if [ ! -e /var/lib/postern/authorized_keys ]; then
	install -o postern -g postern -m 0640 /dev/null /var/lib/postern/authorized_keys
else
	chown postern:postern /var/lib/postern/authorized_keys
	chmod 0640 /var/lib/postern/authorized_keys
fi
if [ ! -e /var/lib/postern/postern.db ]; then
	install -o postern -g postern -m 0640 /dev/null /var/lib/postern/postern.db
else
	chown postern:postern /var/lib/postern/postern.db
	chmod 0640 /var/lib/postern/postern.db
fi

VPS_HOSTNAME=${POSTERN_VPS_HOSTNAME:-}
if [ -z "$VPS_HOSTNAME" ]; then
	VPS_HOSTNAME=$(hostname -f 2>/dev/null || hostname)
fi
echo "$VPS_HOSTNAME" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$' || \
	die "refusing vps_hostname $VPS_HOSTNAME (set POSTERN_VPS_HOSTNAME to a DNS name or IPv4)"
case $VPS_HOSTNAME in
localhost | localhost.localdomain)
	echo "vps-bootstrap: warning: vps_hostname=$VPS_HOSTNAME looks wrong; edit $CONF_DEST after install" >&2
	;;
esac
if [ ! -e "$CONF_DEST" ]; then
	cat >"$CONF_DEST" <<EOF
db_path                 = "/var/lib/postern/postern.db"
socket_path             = "/run/postern/api.sock"
tunnel_user             = "postern"
authorized_keys_path    = "/var/lib/postern/authorized_keys"
port_min                = 2200
port_max                = 2299
heartbeat_offline_after = "90s"
join_token_ttl          = "15m"
vps_hostname            = "$VPS_HOSTNAME"
EOF
	chown root:postern "$CONF_DEST"
	chmod 0640 "$CONF_DEST"
	echo "vps-bootstrap: wrote $CONF_DEST vps_hostname=$VPS_HOSTNAME"
else
	echo "vps-bootstrap: keeping existing $CONF_DEST"
fi

install -d -o root -g root -m 0755 /etc/ssh/sshd_config.d
install -o root -g root -m 0644 "$SSHD_FIXTURE" "$SSHD_DROPIN"
install -o root -g root -m 0644 "$UNIT_FIXTURE" "$UNIT_DEST"

# Hard-fail: never reload ssh with a broken Match drop-in (can lock out $ADMIN).
if ! "$SSHD" -t; then
	echo "vps-bootstrap: sshd -t failed; not reloading ssh" >&2
	exit 1
fi
DUMP=$("$SSHD" -T -C "user=${ADMIN},host=localhost,addr=127.0.0.1") || {
	echo "vps-bootstrap: sshd -T for user=$ADMIN failed; not reloading ssh" >&2
	exit 1
}
if echo "$DUMP" | grep -qi '^forcecommand /usr/bin/posternd-shell'; then
	echo "vps-bootstrap: Match leaked onto $ADMIN (ForceCommand posternd-shell); not reloading ssh" >&2
	exit 1
fi

systemctl reload ssh 2>/dev/null || systemctl reload sshd

systemctl daemon-reload
systemctl enable --now posternd

echo
echo "vps-bootstrap: installed /usr/bin/posternd and posternd-shell symlink"
echo "vps-bootstrap: $ADMIN is in group postern — open a NEW SSH session so SO_PEERGROUPS sees it"
echo "vps-bootstrap: Linux agents need a lingering user session or the tunnel dies on logout:"
echo "               loginctl enable-linger <login-user>"
echo "vps-bootstrap: split-enroll is the canary path (join --token, enroll-machine, join --apply-response)."
echo "               join --submit copies admin SSH onto the machine; it is not the headless path."
echo "vps-bootstrap: MaxStartups was not changed."
echo "Edit $CONF_DEST if vps_hostname is wrong, then: systemctl restart posternd"
