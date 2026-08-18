# デプロイ手順書

この手順では、TerraformでVMとマネージドDBを作成した後、node-01をAnsibleの実行ホストとして利用し、node-02〜05へアプリケーションをデプロイします。

アプリケーション用のDBコンテナは起動しません。Terraformで作成したマネージドDBへ接続します。

## 1. 事前準備

作業端末に以下を用意します。

- Terraform
- Ansible（`ansible-playbook`）
- OpenSSH（`ssh`、`ssh-keygen`）
- さくらのクラウドのAPIトークンとシークレット
- 1Passwordに登録されたVM初期パスワード、Managed DBパスワード、コンテナレジストリ認証情報

以降のコマンドは、リポジトリの `infra` ディレクトリを基準にしています。

```bash
cd infra
```

## 2. SSH鍵を準備する（手動作業）

`deploy.sh` は、作業端末の `~/.ssh/intern28` をnode-01へ転送し、node-01からnode-02〜05へ接続します。

鍵がまだない場合だけ作成します。

```bash
mkdir -p ~/.ssh
ssh-keygen -t ed25519 -f ~/.ssh/intern28 -C sakuravel-ansible
chmod 600 ~/.ssh/intern28
chmod 644 ~/.ssh/intern28.pub
```

すでに `~/.ssh/intern28` がある場合は、上書きしないでください。

## 3. Terraformのシークレットを設定する（手動作業）

Terraform用のローカルファイルを作成します。このファイルはGitへコミットしません。

```bash
cd terraform
cp secret.auto.tfvars.example secret.auto.tfvars
```

`secret.auto.tfvars` に、1Passwordから次の値を記入します。

```hcl
sakura_access_token        = "..."
sakura_access_token_secret = "..."
db_password                = "..."
server_password            = "..."
server_ssh_public_key_path = "~/.ssh/intern28.pub"
```

`server_ssh_public_key_path` は必ず設定してください。VM作成時に公開鍵がcloud-initへ渡され、初回SSH接続に使用されます。

設定後、Terraformを実行します。

```bash
terraform init
terraform plan
terraform apply
```

`apply` 完了後、node-01の公開IPが出力されることを確認します。

```bash
terraform output -raw bastion_public_ip
```

## 4. Ansibleの認証情報を設定する（手動作業）

Ansibleの認証情報ファイルを作成します。このファイルもGitへコミットしません。

```bash
cd ../ansible
cp group_vars/all.example.yml group_vars/all.yml
chmod 600 group_vars/all.yml
```

`group_vars/all.yml` に、1Passwordからレジストリ認証情報とManaged DB接続情報を記入します。

```yaml
registry_host: intern9.sakuracr.jp
registry_username: "..."
registry_password: "..."

db_host: 192.168.1.30
db_port: 3306
db_name: sakuravel_app
db_user: sakuravel_app
db_password: "..."
```

`db_port` は、現在のManaged DB（MariaDB）のポート `3306` を指定します。DBの接続情報を変更した場合は、実際の値に合わせてください。

`node_private_ips` はTerraformの構成と一致させます。標準構成では変更不要です。

認証情報を含むファイルを表示・共有・コミットしないでください。

## 5. アプリケーションをデプロイする（自動実行）

VM作成後、次の1コマンドだけを実行します。

```bash
./deploy.sh
```

`deploy.sh` が以下を自動実行します。

1. Terraform stateからnode-01の公開IPを取得
2. SSH鍵と `group_vars/all.yml` の存在を確認
3. node-01へAnsible、設定ファイル、SSH秘密鍵を配置
4. node-01のIP forwardingとNATを設定
5. node-01からnode-02〜05へのSSH接続を準備
6. node-02〜05へDocker、レジストリログイン、アプリケーションを設定
7. Managed DBを使用するComposeを起動

node-01へログインしてファイルを手作業で編集したり、inventoryのIPを書き換えたりする必要はありません。

## 6. デプロイ結果を確認する

`deploy.sh` の最後に、node-02〜05のAnsible recapで `failed=0` と `unreachable=0` を確認します。

必要に応じて、Terraformの出力からnode-01へ接続します。

```bash
cd ../terraform
ssh -i ~/.ssh/intern28 ubuntu@"$(terraform output -raw bastion_public_ip)"
```

node-01からアプリケーションの状態を確認する場合は、Ansible実行後に次を実行します。

```bash
cd /opt/sakuravel-ansible
ansible-playbook site.yml
```

## 7. SSH公開鍵をVM作成後に追加する場合

通常は `server_ssh_public_key_path` を設定してから `terraform apply` するため、この作業は不要です。

VM作成時に公開鍵を設定し忘れた場合は、1PasswordのVM初期パスワードを使って、各VMへ一度だけ鍵を手動配布します。その後は `./deploy.sh` を実行できます。

```bash
cd ../terraform
BASTION=$(terraform output -raw bastion_public_ip)
ssh-copy-id -i ~/.ssh/intern28.pub ubuntu@"${BASTION}"

for i in 2 3 4 5; do
  ssh-copy-id -i ~/.ssh/intern28.pub \
    -o ProxyJump=ubuntu@"${BASTION}" \
    ubuntu@192.168.1.1"${i}"
done
```

## 8. よくある問題

### `missing: .../group_vars/all.yml`

次を実行して、ローカルの認証情報ファイルを作成・編集します。

```bash
cd infra/ansible
cp group_vars/all.example.yml group_vars/all.yml
chmod 600 group_vars/all.yml
```

### `missing: ~/.ssh/intern28`

SSH秘密鍵を配置するか、まだ作成していなければ「2. SSH鍵を準備する」を実行します。

### `Permission denied (publickey)`

`secret.auto.tfvars` の `server_ssh_public_key_path` が正しいか確認します。VM作成後に値を変更しただけでは既存VMへ鍵は追加されないため、「7. SSH公開鍵をVM作成後に追加する」を実行します。

### DBへ接続できない

次を確認します。

- `group_vars/all.yml` の `db_host`、`db_port`、`db_name`、`db_user`、`db_password`
- Managed DBの接続許可設定
- Managed DBが起動済みであること
- node-01のNATとnode-02〜05のデフォルトルート

### 再デプロイ

設定を修正した後、同じコマンドを再実行します。Ansibleは現在の状態との差分だけを適用します。

```bash
cd infra/ansible
./deploy.sh
```
