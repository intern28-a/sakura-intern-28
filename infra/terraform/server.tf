########################################
# サーバー
########################################

data "sakura_archive" "ubuntu" {
  name = "Ubuntu Server 24.04.2 LTS 64bit (cloudimg)"
  zone = var.zone
}

# プライベート専用ノードの netplan に入れる DNS サーバのアドレス
data "sakura_zone" "current" {
  name = var.zone
}

resource "sakura_disk" "node" {
  count = var.node_count

  name              = format("%s-%02d-disk", var.node_name_prefix, count.index + 1)
  plan              = var.disk_plan
  size              = var.disk_size
  source_archive_id = data.sakura_archive.ubuntu.id
  zone              = var.zone
}

resource "sakura_server" "node" {
  count = var.node_count

  name   = format("%s-%02d", var.node_name_prefix, count.index + 1)
  disks  = [sakura_disk.node[count.index].id]
  core   = var.server_core
  memory = var.server_memory
  zone   = var.zone

  # 共有セグメント (グローバルIP) を持つのは node-01 のみ。
  # node-02〜05 は app スイッチ1本だけで、外部通信は node-01 の NAT を経由する。
  network_interface = concat(
    count.index == 0 ? [{ upstream = "shared" }] : [],
    [{ upstream = sakura_vswitch.app.id }],
  )

  # cloud-init user-data
  user_data = templatefile("${path.module}/cloud-init.yaml.tftpl", {
    hostname       = format("%s-%02d", var.node_name_prefix, count.index + 1)
    password       = var.server_password
    ssh_public_key = var.server_ssh_public_key_path != "" ? file(pathexpand(var.server_ssh_public_key_path)) : ""

    private_ip    = local.node_private_ips[count.index]
    prefix_length = local.app_prefix_length

    # node-01 だけが NAT ゲートウェイとして振る舞う
    is_gateway   = count.index == 0
    gateway_ip   = local.gateway_private_ip
    private_cidr = local.app_net_cidr
    dns_servers  = data.sakura_zone.current.dns_servers
  })
}

resource "sakura_ssh_key" "foobar" {
  name        = "localsshkey"
  public_key  = file(pathexpand("~/.ssh/id_ed25519.pub"))
  description = "node-01から02~05へのsshをするための公開鍵"
}
