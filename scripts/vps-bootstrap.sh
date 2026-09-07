#!/bin/sh
# Install posternd on a Debian VPS: tunnel user, dirs, sshd drop-in, systemd.
# posternd listens on a Unix socket only. Do not add a TCP bind.
#
# Usage (as root):
#   POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh [admin-user]
#   POSTERND_BIN=./posternd ./scripts/vps-bootstrap.sh --create debian
#
# admin-user is the login you SSH as (added to group postern). It must not be
# root or postern. With no argument, the script uses $SUDO_USER, then debian,
# ubuntu, or the first uid>=1000 login. uid 0 is already an API admin.
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
SSHD_CONFIG=/etc/ssh/sshd_config
UNIT_DEST=/etc/systemd/system/posternd.service
CONF_DEST=/etc/postern/posternd.toml
POSTERN_MATCH_BEGIN="# BEGIN POSTERN MATCH"
POSTERN_MATCH_END="# END POSTERN MATCH"

CREATE=0
ADMIN=""
for arg in "$@"; do
	case $arg in
	--create)
		CREATE=1
		;;
	-*)
		die "unknown flag $arg (usage: $0 [--create] [admin-user])"
		;;
	root)
		die "admin user must not be root (uid 0 is already an API admin; pass the login you SSH as)"
		;;
	postern)
		die "admin user must not be postern (uid postern is never admin)"
		;;
	*)
		if [ -n "$ADMIN" ]; then
			die "too many arguments: $ADMIN $arg"
		fi
		ADMIN=$arg
		;;
	esac
done

list_logins() {
	awk -F: '$3 >= 1000 && $3 < 65534 && $1 != "nobody" && $1 != "nfsnobody" && $7 !~ /nologin|false/ { print $1 }' /etc/passwd
}

login_exists() {
	[ -n "$1" ] && getent passwd "$1" >/dev/null
}

create_admin() {
	name=$1
	echo "vps-bootstrap: creating login $name"
	useradd --create-home --shell /bin/bash --comment "Postern admin" "$name" || \
		die "useradd $name failed"
	if getent group sudo >/dev/null; then
		usermod -aG sudo "$name"
	fi
	if getent group wheel >/dev/null; then
		usermod -aG wheel "$name"
	fi
	home=$(getent passwd "$name" | cut -d: -f6)
	[ -n "$home" ] || die "no home for $name"
	if [ -s /root/.ssh/authorized_keys ]; then
		install -d -o "$name" -g "$name" -m 0700 "$home/.ssh"
		install -o "$name" -g "$name" -m 0600 /root/.ssh/authorized_keys "$home/.ssh/authorized_keys"
		echo "vps-bootstrap: copied /root/.ssh/authorized_keys to $home/.ssh/authorized_keys"
	else
		echo "vps-bootstrap: warning: no /root/.ssh/authorized_keys to copy; add an SSH pubkey before leaving root" >&2
	fi
}

pick_existing_admin() {
	if [ -n "${SUDO_USER-}" ] && [ "$SUDO_USER" != "root" ] && [ "$SUDO_USER" != "postern" ] && login_exists "$SUDO_USER"; then
		echo "$SUDO_USER"
		return
	fi
	for c in debian ubuntu admin; do
		if login_exists "$c"; then
			echo "$c"
			return
		fi
	done
	list_logins | head -n 1
}

if [ -n "$ADMIN" ]; then
	if ! login_exists "$ADMIN"; then
		if [ "$CREATE" -eq 1 ]; then
			create_admin "$ADMIN"
		else
			echo "vps-bootstrap: admin user $ADMIN does not exist" >&2
			echo "vps-bootstrap: existing logins:" >&2
			logins=$(list_logins)
			if [ -n "$logins" ]; then
				echo "$logins" | sed 's/^/  /' >&2
				first=$(echo "$logins" | head -n 1)
				echo "vps-bootstrap: re-run with one of those, e.g. $0 $first" >&2
			else
				echo "  (none — this looks like a root-only image)" >&2
			fi
			echo "vps-bootstrap: or create $ADMIN: $0 --create $ADMIN" >&2
			exit 1
		fi
	fi
else
	ADMIN=$(pick_existing_admin || true)
	if [ -z "$ADMIN" ]; then
		if [ "$CREATE" -eq 1 ]; then
			ADMIN=debian
			create_admin "$ADMIN"
		else
			die "no admin login found (root-only image). Create one: $0 --create debian"
		fi
	fi
	echo "vps-bootstrap: using admin login $ADMIN"
fi

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
# Debian Include is at the top of sshd_config. A Match block in a drop-in
# wraps every later keyword (UsePAM, PermitRootLogin, …) and can lock out
# root. Put only global keywords in the drop-in; append Match at EOF.
SSHD_DROPIN_PREV=""
SSHD_CONFIG_PREV=""
if [ -f "$SSHD_DROPIN" ]; then
	SSHD_DROPIN_PREV=$(mktemp)
	cp -p "$SSHD_DROPIN" "$SSHD_DROPIN_PREV"
fi
if [ -f "$SSHD_CONFIG" ]; then
	SSHD_CONFIG_PREV=$(mktemp)
	cp -p "$SSHD_CONFIG" "$SSHD_CONFIG_PREV"
else
	die "missing $SSHD_CONFIG"
fi

GLOBALS=$(mktemp)
MATCH=$(mktemp)
awk -v globf="$GLOBALS" -v matchf="$MATCH" '
	/^Match User postern$/ { p = 1 }
	p { print > matchf; next }
	{ print > globf }
' "$SSHD_FIXTURE"
grep -q '^Match User postern$' "$MATCH" || die "fixture missing Match User postern"
grep -q '^Match all$' "$MATCH" || die "fixture missing Match all"
grep -q '^PermitUserEnvironment' "$GLOBALS" || die "fixture missing PermitUserEnvironment"

install -o root -g root -m 0644 "$GLOBALS" "$SSHD_DROPIN"
rm -f "$GLOBALS"
if grep -q "^${POSTERN_MATCH_BEGIN}$" "$SSHD_CONFIG"; then
	sed -i "/^${POSTERN_MATCH_BEGIN}$/,/^${POSTERN_MATCH_END}$/d" "$SSHD_CONFIG"
fi
# Ensure the Match is the last thing parsed (Include-at-top cannot wrap it).
printf '\n%s\n' "$POSTERN_MATCH_BEGIN" >>"$SSHD_CONFIG"
cat "$MATCH" >>"$SSHD_CONFIG"
printf '%s\n' "$POSTERN_MATCH_END" >>"$SSHD_CONFIG"
rm -f "$MATCH"
chmod 0644 "$SSHD_CONFIG"

install -o root -g root -m 0644 "$UNIT_FIXTURE" "$UNIT_DEST"

rollback_sshd() {
	if [ -n "${SSHD_DROPIN_PREV-}" ] && [ -f "$SSHD_DROPIN_PREV" ]; then
		cp -p "$SSHD_DROPIN_PREV" "$SSHD_DROPIN"
		rm -f "$SSHD_DROPIN_PREV"
	else
		rm -f "$SSHD_DROPIN"
	fi
	if [ -n "${SSHD_CONFIG_PREV-}" ] && [ -f "$SSHD_CONFIG_PREV" ]; then
		cp -p "$SSHD_CONFIG_PREV" "$SSHD_CONFIG"
		rm -f "$SSHD_CONFIG_PREV"
	fi
}

abort_sshd() {
	echo "vps-bootstrap: $1; not reloading ssh" >&2
	rollback_sshd
	exit 1
}

sshd_dump() {
	"$SSHD" -T -C "user=${1},host=localhost,addr=127.0.0.1"
}

assert_not_tunnel_user() {
	who=$1
	dump=$(sshd_dump "$who") || abort_sshd "sshd -T for user=$who failed"
	if echo "$dump" | grep -qi '^forcecommand /usr/bin/posternd-shell'; then
		abort_sshd "Match leaked onto $who (ForceCommand posternd-shell)"
	fi
	if echo "$dump" | grep -qi '^permitty no$'; then
		abort_sshd "Match leaked onto $who (PermitTTY no)"
	fi
	if echo "$dump" | grep -qi '^authorizedkeysfile .*/var/lib/postern/authorized_keys'; then
		abort_sshd "Match leaked onto $who (AuthorizedKeysFile postern)"
	fi
}

# Hard-fail: never reload ssh with a broken Match (can lock out root/$ADMIN).
if ! "$SSHD" -t; then
	abort_sshd "sshd -t failed"
fi
assert_not_tunnel_user root
assert_not_tunnel_user "$ADMIN"
POSTERN_DUMP=$(sshd_dump postern) || abort_sshd "sshd -T for user=postern failed"
echo "$POSTERN_DUMP" | grep -qi '^forcecommand /usr/bin/posternd-shell' || \
	abort_sshd "Match User postern did not set ForceCommand"
ROOT_PRL=$(sshd_dump root | awk 'tolower($1)=="permitrootlogin"{print $2; exit}')
if [ "$ROOT_PRL" = "no" ]; then
	echo "vps-bootstrap: warning: PermitRootLogin no — SSH as $ADMIN, not root" >&2
fi

rm -f "$SSHD_DROPIN_PREV" "$SSHD_CONFIG_PREV"

systemctl reload ssh 2>/dev/null || systemctl reload sshd

systemctl daemon-reload
systemctl enable --now posternd

echo
echo "vps-bootstrap: installed /usr/bin/posternd and posternd-shell symlink"
echo "vps-bootstrap: $ADMIN is in group postern — open a NEW SSH session so SO_PEERGROUPS sees it"
echo "vps-bootstrap: from a laptop, postern config set server ${ADMIN}@${VPS_HOSTNAME}"
echo "vps-bootstrap: Linux agents need a lingering user session or the tunnel dies on logout:"
echo "               loginctl enable-linger <login-user>"
echo "vps-bootstrap: split-enroll is the canary path (join --token, enroll-machine, join --apply-response)."
echo "               join --submit copies admin SSH onto the machine; it is not the headless path."
echo "vps-bootstrap: MaxStartups was not changed."
echo "Edit $CONF_DEST if vps_hostname is wrong, then: systemctl restart posternd"
