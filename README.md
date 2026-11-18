# NWログ取得マクロ（TeraTerm）

ネットワーク機器のログを、Tera Term 経由で **複数台まとめて・同時に** 取得する Windows 用ツールです。
ホストリストの編集、取得コマンドの選択、取得開始までを 1 画面で行えます。

- **並列取得** — 最大 200 台を同時に取得。`show tech` のような長いコマンドも最後まで取得します
- **機器種別に対応** — Cisco IOS / FortiGate / YAMAHA RTX / 汎用。プロンプトや特権モードの入り方を種別ごと・ホストごとに設定できます
- **Excel から貼り付け** — ホストリストは Excel の範囲をコピーして貼り付けるだけ。旧マクロの `host_list.csv` もそのまま読み込めます
- **プロファイル** — 拠点・案件ごとのホストリストをプルダウンで切り替え
- **インストール不要** — EXE を置いてダブルクリックするだけ。新しいバージョンが出ると起動時にお知らせし、そのまま更新できます

## 動作環境

| 項目 | 内容 |
|---|---|
| OS | Windows 10 / 11（64bit） |
| Tera Term | 4.x または 5.x（別途インストールが必要です。本ツールには同梱していません） |
| ブラウザ | Microsoft Edge（無い場合は Google Chrome）※画面の表示に使います |

Tera Term は [Tera Term Project](https://teratermproject.github.io/) から入手してください。

## ダウンロードと起動

1. [Releases](https://github.com/yukkunn611/nwlog-teraterm/releases/latest) から `nwlog-teraterm_vX.Y.Z.zip` をダウンロードして、好きな場所に展開します。
2. `NWログ取得マクロ（TeraTerm）.exe` をダブルクリックします。
   - 「Windows によって PC が保護されました」と表示された場合は、**「詳細情報」→「実行」** で起動できます（コード署名のない EXE のため）。
3. 初回は利用規約・免責事項への同意画面が出ます。
4. 右上の「⚙ 詳細設定」で Tera Term の `ttpmacro.exe` の場所を指定します（標準の場所なら自動で見つかります）。

使い方の詳細は、アプリ右上の **「💡 使い方」** を開いてください。

## 使い方の流れ

1. **ホストリスト** を入力する（Excel から貼り付け可）。各ホストの「機器種別」を選ぶ
2. **取得コマンド** のセットを作る（1 行 1 コマンド）
3. **取得設定**（ログ識別子・保存先・並列 / 連続）を決める
4. **▶ 取得開始**

ログは `識別子_ホスト名_YYYYMMDD-HHMMSS.txt` の名前で保存されます。

## 注意事項

- パスワードなどの認証情報は、EXE と同じ場所の `data` フォルダに **暗号化されずに保存** されます。他の人にツールを渡すときは `data` フォルダを含めないでください。
- 取得中は一時フォルダ（`%TEMP%\nwlog_tt`）にパスワード入りのマクロが置かれます。終了時・次回起動時に削除します。
- 「SSH 初回接続の確認ダイアログを出さない」は既定で ON です（並列取得が確認ダイアログで止まらないようにするため）。ホスト鍵の確認が省略されるので、信頼できるネットワークでのみ使ってください。
- 利用前に [利用規約・免責事項](TERMS.md) をお読みください。

## 通信について

画面はこの PC の中（127.0.0.1）だけで動作します。外部への通信は、更新の確認とダウンロードのための GitHub へのアクセスのみで、ホストリストや認証情報・ログを送ることはありません。更新の確認は詳細設定で OFF にできます。

## 不具合の報告・要望

[Issues](https://github.com/yukkunn611/nwlog-teraterm/issues) へお寄せください。無償で公開しているツールのため、対応はお約束できません。報告の際は、詳細設定で「デバッグモード」を ON にして再現させ、`data\debug\run_日時` フォルダの内容を添えていただくと原因を追いやすくなります（IP アドレスやホスト名が含まれるので、必要に応じて伏せてください）。

## 開発者向け

### ビルド

Go 1.24 以降が必要です。外部ライブラリは使っていません。

```bat
go build -trimpath -ldflags "-H=windowsgui -s -w" -o "NWログ取得マクロ（TeraTerm）.exe" .
```

Linux / macOS からのクロスビルド:

```sh
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w" -o nwlog-teraterm.exe .
```

アイコンは `rsrc_windows_amd64.syso` に埋め込み済みです。差し替える場合は [rsrc](https://github.com/akavel/rsrc) で作り直してください。

### 構成

| ファイル | 内容 |
|---|---|
| `main.go` | 起動、画面用の小さな Web サーバー（127.0.0.1 のみ） |
| `api.go` | 画面から呼ばれる API |
| `runner.go` | 取得ジョブの実行（並列制御・進捗監視・タイムアウト） |
| `ttl.go` | 1 台ぶんの Tera Term マクロ（TTL）の生成 |
| `devtype.go` | 機器種別ごとの既定値 |
| `update.go` | 更新の確認と自動更新 |
| `store.go` | 設定・プロファイル・コマンドセットの保存 |
| `sys_windows.go` | Windows 固有の処理（文字コード、プロセス管理、ダイアログ、プロキシ） |
| `web/` | 画面（HTML / CSS / JavaScript）。`changelog.json` が更新履歴の元データ |

### リリース手順

1. `web/changelog.json` の先頭に新しいバージョンの項目を追加し、`main.go` の `appVersion` も合わせて更新する
2. コミットしてプッシュする
3. `v` 付きのタグ（例: `v1.3.1`）を作ってプッシュする

```sh
git tag v1.3.1
git push origin v1.3.1
```

GitHub Actions（`.github/workflows/release.yml`）が自動で EXE をビルドし、`changelog.json` の内容をリリースノートにして Release を作成します。アプリはこの Release を見て更新を知らせます。

## ライセンス

[MIT License](LICENSE) © 2026 Yuki Kayamura

## 商標

Cisco、Cisco IOS は Cisco Systems, Inc.、FortiGate は Fortinet, Inc.、YAMAHA はヤマハ株式会社、Windows、Microsoft Edge は Microsoft Corporation の、それぞれ米国その他の国における商標または登録商標です。Tera Term は TeraTerm Project の著作物です。本ツールは、これらの企業・団体とは関係のない個人が開発したものです。
