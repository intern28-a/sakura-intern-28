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

  name              = "${local.node_names[count.index]}-disk"
  plan              = var.disk_plan
  size              = var.disk_size
  source_archive_id = data.sakura_archive.ubuntu.id
  zone              = var.zone
}

resource "sakura_server" "node" {
  count = var.node_count

  name   = local.node_names[count.index]
  disks  = [sakura_disk.node[count.index].id]
  core   = var.server_core
  memory = var.server_memory
  zone   = var.zone

  # グローバル側 (共有セグメント / ルータ+スイッチ) は【必ず NIC[0]】に置く。
  # API 制約:
  #   "Only the first interface can be connected to router+switches or shared segments"
  # 2本目以降に置くと 400 bad_request で作成に失敗する。
  #
  # 結果、どのノードも並びは同じ形になる。これがそのまま OS 側の
  # enumeration 順 (ifindex 順) になり、cloud-init が NIC を判別する根拠になる。
  #   edge        : [0] 共有セグメント    [1] app スイッチ
  #   node-02〜05 : [0] ルータ+スイッチ   [1] app スイッチ
  network_interface = concat(
    count.index == 0 ? [{
      upstream         = "shared"
      packet_filter_id = null
    }] : [],
    contains(local.app_node_indexes, count.index) ? [{
      upstream = sakura_internet.pub.vswitch_id
      # グローバル側だけフィルタする。app スイッチ側は素通しなので、
      # 踏み台経由の SSH は従来通り通る。
      packet_filter_id = sakura_packet_filter.node[local.node_packet_filter_role[count.index]].id
    }] : [],
    [{
      upstream         = sakura_vswitch.app.id
      packet_filter_id = null
    }],
  )

  # cloud-init user-data
  user_data = templatefile("${path.module}/cloud-init.yaml.tftpl", {
    hostname       = local.node_names[count.index]
    password       = var.server_password
    ssh_public_key = var.server_ssh_public_key_path != "" ? file(pathexpand(var.server_ssh_public_key_path)) : ""

    # app スイッチ側。デフォルトルートは持たせない。
    private_ip            = local.node_private_ips[count.index]
    private_prefix_length = local.app_prefix_length

    # ルータ+スイッチ側。edge は載らないので null になる。
    # さくらのルータ+スイッチは DHCP を提供しないため、静的に設定する。
    has_public_nic       = contains(local.app_node_indexes, count.index)
    public_ip            = local.node_public_ips[count.index]
    public_prefix_length = sakura_internet.pub.netmask
    public_gateway       = sakura_internet.pub.gateway

    dns_servers = data.sakura_zone.current.dns_servers

    # DSR 実サーバ設定。lo に付ける VIP と待ち受けポートは役割で変わる。
    # local 側で算出しているので sakura_dsr_lb への依存は生まれず、
    # 循環参照にはならない。
    dsr_vip  = local.node_dsr_vip[count.index]
    dsr_port = local.node_service_port[count.index]
  })
}

resource "sakura_ssh_key" "foobar" {
  name        = "localsshkey"
  public_key  = file(pathexpand("~/.ssh/intern28.pub"))
  description = "nodeへのsshをするための公開鍵"
}
