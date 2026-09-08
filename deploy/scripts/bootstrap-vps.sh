#!/usr/bin/env bash
#
# Prepares a fresh Ubuntu 24.04 VPS (Hetzner or equivalent) to run the
# Meterrail backend. Run once, as root, on a new host:
#
#   scp deploy/scripts/bootstrap-vps.sh root@HOST:/tmp/
#   ssh root@HOST 'bash /tmp/bootstrap-vps.sh deploy'
#
# It installs Docker, creates an unprivileged deploy user, locks down SSH and
# configures the firewall. It is idempotent: re-running it is safe.

set -euo pipefail

DEPLOY_USER="${1:-deploy}"
APP_DIR="/opt/meterrail"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "run this as root"

log "Updating packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get upgrade -y -qq
apt-get install -y -qq \
    ca-certificates curl gnupg git ufw fail2ban unattended-upgrades \
    apache2-utils jq

log "Installing Docker"
if ! command -v docker >/dev/null 2>&1; then
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
        | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg

    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
        > /etc/apt/sources.list.d/docker.list

    apt-get update -qq
    apt-get install -y -qq \
        docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi
systemctl enable --now docker

log "Creating deploy user '${DEPLOY_USER}'"
if ! id -u "$DEPLOY_USER" >/dev/null 2>&1; then
    adduser --disabled-password --gecos '' "$DEPLOY_USER"
fi
usermod -aG docker "$DEPLOY_USER"

# Carry root's authorised keys over so the new user can log in immediately.
if [[ -f /root/.ssh/authorized_keys ]]; then
    install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "/home/${DEPLOY_USER}/.ssh"
    install -m 600 -o "$DEPLOY_USER" -g "$DEPLOY_USER" \
        /root/.ssh/authorized_keys "/home/${DEPLOY_USER}/.ssh/authorized_keys"
fi

log "Creating ${APP_DIR}"
install -d -m 755 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$APP_DIR"
install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "${APP_DIR}/secrets"

log "Configuring the firewall"
ufw --force reset >/dev/null
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp comment 'SSH'
ufw allow 80/tcp comment 'HTTP'
ufw allow 443/tcp comment 'HTTPS'
ufw --force enable

log "Hardening SSH"
# Password auth and root login are the two doors that actually get brute-forced.
sed -i \
    -e 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' \
    -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' \
    -e 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' \
    /etc/ssh/sshd_config
systemctl reload ssh || systemctl reload sshd

log "Enabling fail2ban and unattended upgrades"
systemctl enable --now fail2ban
dpkg-reconfigure -f noninteractive unattended-upgrades

log "Tuning kernel limits for a proxy workload"
cat > /etc/sysctl.d/99-meterrail.conf <<'SYSCTL'
# Larger accept queue so bursts are not dropped before nginx accepts them.
net.core.somaxconn = 4096
net.ipv4.tcp_max_syn_backlog = 4096
# Reuse TIME_WAIT sockets for outbound connections to the database and R2.
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 10240 65535
fs.file-max = 200000
SYSCTL
sysctl --system >/dev/null

cat <<EOF

$(log "Bootstrap complete")

Next steps:
  1. Copy the production environment file:
       scp .env.production ${DEPLOY_USER}@HOST:${APP_DIR}/.env.production
  2. Copy the deploy directory:
       scp -r deploy ${DEPLOY_USER}@HOST:${APP_DIR}/
  3. Point your DNS A records at this host.
  4. Issue certificates:
       ssh ${DEPLOY_USER}@HOST '${APP_DIR}/deploy/scripts/issue-cert.sh api.example.com you@example.com'
  5. Deploy:
       make deploy-api DEPLOY_HOST=${DEPLOY_USER}@HOST

EOF
