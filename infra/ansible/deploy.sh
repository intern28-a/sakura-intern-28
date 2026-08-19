#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_DIR="${SCRIPT_DIR}/../terraform"
SSH_KEY="${HOME}/.ssh/intern28"

test -f "${SCRIPT_DIR}/group_vars/all.yml" || {
  echo "missing: ${SCRIPT_DIR}/group_vars/all.yml" >&2
  exit 1
}
test -f "${SSH_KEY}" || {
  echo "missing: ${SSH_KEY}" >&2
  exit 1
}

BASTION_IP="$(terraform -chdir="${TERRAFORM_DIR}" output -raw bastion_public_ip)"

# ノードの役割・アドレスは Terraform を単一の出典にする。
# ここで拾ったものが bootstrap.yml 経由で controller の group_vars/app.yml と
# インベントリに流れ、site.yml の netplan / compose まで届く。
APP_URL="$(terraform -chdir="${TERRAFORM_DIR}" output -raw app_url)"
API_URL="$(terraform -chdir="${TERRAFORM_DIR}" output -raw api_url)"
NODE_PRIVATE_IPS="$(terraform -chdir="${TERRAFORM_DIR}" output -json node_private_ips)"
NODE_PUBLIC_IPS="$(terraform -chdir="${TERRAFORM_DIR}" output -json node_public_ips)"
NODE_ROLES="$(terraform -chdir="${TERRAFORM_DIR}" output -json node_roles)"
DNS_SERVERS="$(terraform -chdir="${TERRAFORM_DIR}" output -json dns_servers)"
PUB_NETWORK="$(terraform -chdir="${TERRAFORM_DIR}" output -json pub_network)"
APP_NET_CIDR="$(terraform -chdir="${TERRAFORM_DIR}" output -raw app_net_cidr)"
LB_FRONTEND="$(terraform -chdir="${TERRAFORM_DIR}" output -json lb_frontend)"
LB_API="$(terraform -chdir="${TERRAFORM_DIR}" output -json lb_api)"
DB_HOST="$(terraform -chdir="${TERRAFORM_DIR}" output -raw db_host)"

# jq に依存しないよう、JSON をそのまま埋め込んで組み立てる
TF_EXTRA_VARS="$(cat <<JSON
{
  "app_url": "${APP_URL}",
  "api_url": "${API_URL}",
  "node_private_ips": ${NODE_PRIVATE_IPS},
  "node_public_ips": ${NODE_PUBLIC_IPS},
  "node_roles": ${NODE_ROLES},
  "dns_servers": ${DNS_SERVERS},
  "pub_network": ${PUB_NETWORK},
  "app_net_cidr": "${APP_NET_CIDR}",
  "lb_frontend": ${LB_FRONTEND},
  "lb_api": ${LB_API},
  "db_host": "${DB_HOST}"
}
JSON
)"

ANSIBLE_LOCAL_TEMP="${TMPDIR:-/tmp}/sakuravel-ansible"
ANSIBLE_CONFIG="${SCRIPT_DIR}/ansible.cfg"
export ANSIBLE_LOCAL_TEMP
export ANSIBLE_CONFIG

ansible-playbook \
  -i "${BASTION_IP}," \
  -e "ansible_user=ubuntu" \
  -e "ansible_ssh_private_key_file=${SSH_KEY}" \
  -e "${TF_EXTRA_VARS}" \
  "${SCRIPT_DIR}/bootstrap.yml"

ssh -o StrictHostKeyChecking=accept-new -i "${SSH_KEY}" "ubuntu@${BASTION_IP}" \
  'cd /opt/sakuravel-ansible && ansible-playbook site.yml'
