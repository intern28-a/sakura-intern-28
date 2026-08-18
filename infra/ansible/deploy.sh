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
ANSIBLE_LOCAL_TEMP="${TMPDIR:-/tmp}/sakuravel-ansible"
ANSIBLE_CONFIG="${SCRIPT_DIR}/ansible.cfg"
export ANSIBLE_LOCAL_TEMP
export ANSIBLE_CONFIG

ansible-playbook \
  -i "${BASTION_IP}," \
  -e "ansible_user=ubuntu" \
  -e "ansible_ssh_private_key_file=${SSH_KEY}" \
  "${SCRIPT_DIR}/bootstrap.yml"

ssh -o StrictHostKeyChecking=accept-new -i "${SSH_KEY}" "ubuntu@${BASTION_IP}" \
  'cd /opt/sakuravel-ansible && ansible-playbook site.yml'
