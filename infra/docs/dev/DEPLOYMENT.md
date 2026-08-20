# デプロイ手順書

まずこのリポジトリをcloneしておいてください
```bash
git clone https://github.com/intern28-a/sakura-intern-28
```

## 1. 事前準備

作業端末に以下を用意します。

- Terraform
- Ansible
  - [インストール方法](https://docs.ansible.com/projects/ansible/latest/installation_guide/intro_installation.html#pip-install)
  - macを使用している方は`brew install ansible`でも可
- OpenSSH
- Docker
- さくらのクラウドのAPIトークンとシークレット
- 1Passwordに登録されたシークレット情報

## 2. コンテナレジストリへのイメージ登録
レジストリへのログイン
```bash
$ cd sakura-intern-28/app/backend
$ docker login <作成したコンテナレジストリのホスト名>
Username: [1Password記載の コンテナレジストリ ユーザ名(新規作成用) ]
Password: [1Password記載の コンテナレジストリ パスワード(新規作成用)]
Login Succeeded
```

apiイメージのpush
```bash
docker buildx build \
--platform linux/amd64 \
-t <作成したコンテナレジストリのホスト名>/intern2026-app-backend:latest \
--push \
.
```

フロントイメージのpullとpush
* ログイン（参加者用）
```bash
docker login intern22.sakuracr.jp
Username: guest
Password: [1Password記載の コンテナレジストリ(参加者用)]
Login Succeeded
```

* イメージのpull
```bash
docker pull --platform linux/amd64 intern22.sakuracr.jp/intern2026-app-frontend:latest
```
* イメージのpush
```bash
docker tag intern22.sakuracr.jp/intern2026-app-frontend:latest <作成したコンテナレジストリのホスト名>/intern2026-app-frontend:latest
docker push <作成したコンテナレジストリのホスト名>/intern2026-app-frontend:latest
```

## 3. SSH鍵を準備する

パスワードは空のままEnterを押してください
```bash
mkdir -p ~/.ssh
ssh-keygen -t ed25519 -f ~/.ssh/intern28 -C sakuravel-ansible
chmod 600 ~/.ssh/intern28
chmod 644 ~/.ssh/intern28.pub
```

すでに `~/.ssh/intern28` がある場合は、上書きしないでください。

## 4. Terraformのシークレットを設定する

Terraform用のローカルファイルを作成します。このファイルはGitへコミットしません。

```bash
cd ./terraform
cp secret.auto.tfvars.example secret.auto.tfvars
```

`secret.auto.tfvars` に必要なシークレット値を入力してください

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

## 5. Ansibleの認証情報を設定する

Ansibleの認証情報ファイルを作成します。このファイルもGitへコミットしません。

```bash
cd ../ansible
cp group_vars/all.example.yml group_vars/all.yml
chmod 600 group_vars/all.yml
```

`group_vars/all.yml` に、1Passwordからレジストリ認証情報とManaged DB接続情報を記入します。

```yaml
# DNSを設定してドメインでもアクセスする場合の例
# tls_domains:
#   - teama.intern28.sakuraha.jp
```

`db_port` は、Terraformで作成するMariaDBの接続ポート `3306` を指定します。DBの接続情報を変更した場合は、実際の値に合わせてください。

## 6. アプリケーションをデプロイする

```bash
./deploy.sh
```

## トラブルシューティング

1. terraform　apply実行中にリソース上限のエラーが発生した場合

```bash
terraform destroy
```
を実行してから，不要なリソースが残っていないかを確認してください

2. ansibleの実行中にssh接続のエラーが発生した場合

WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! のような表示があれば，
`~/.ssh/known_hosts`
を編集して，該当するドメインに関する記述を削除してください
