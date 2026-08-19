# bmanga-core

[English](README.md) · [简体中文](README.zh-CN.md) · **日本語**

**あなたが利用権限を持つローカルの漫画・コミックフォルダーを、検索可能な本棚、
ビューアー、読書進捗を備えたローカルファーストのセルフホスト型 Web ライブラリに変えます。
同梱の Compose プロファイルは元のライブラリを読み取り専用でマウントします。**

[GHCR で5分間試す](#5-minute-ghcr-trial) ·
[不具合を報告][bug-report] · [質問する][discussions]

[![Alpha release](https://img.shields.io/github/v/release/zzzcws/bmanga-core?include_prereleases&sort=semver&label=alpha)](https://github.com/zzzcws/bmanga-core/releases/tag/v0.1.0-alpha.3)
[![CI](https://github.com/zzzcws/bmanga-core/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zzzcws/bmanga-core/actions/workflows/ci.yml)
[![Container](https://img.shields.io/badge/GHCR-linux%2Famd64-2496ED?logo=docker&logoColor=white)](https://github.com/zzzcws/bmanga-core/pkgs/container/bmanga-core)
[![License](https://img.shields.io/github/license/zzzcws/bmanga-core)](LICENSE)

> [!WARNING]
> bmanga-core は現在 **Alpha プレビュー版** です。公開コンテナーは現時点で
> **Linux/amd64 のみ** をサポートしています。インターネットに直接公開しないでください。

> [!NOTE]
> 公開中の `v0.1.0-alpha.3` イメージでは、English / 简体中文 / 日本語 の
> インターフェースを選択できます。この設定で切り替わるのは UI の文言のみで、
> 書籍の内容やカタログのメタデータは翻訳されません。明示的に選択するまでは、
> 簡体字中国語が安全な既定値です。

![デスクトップのホーム画面](docs/assets/home-desktop.png)

**その他の合成デモ画面：**

![デスクトップのライブラリ](docs/assets/library-desktop.png)

![モバイルのライブラリ](docs/assets/library-mobile.png)

_スクリーンショットのメタデータとアートワークは、すべて合成デモ用です。bmanga-core には、
インポート可能な書籍、単体の表紙画像、サンプルアーカイブは同梱されていません。
画面は簡体字中国語で表示されていますが、言語は設定画面で切り替えられます。_

## 公開コアでできること

- 利用権限のあるローカル画像フォルダーと対応画像アーカイブを、ローカルの
  SQLite カタログに登録します。
- Web 本棚、カタログ検索、作品詳細、画像ビューアー、ローカルの読書進捗を提供します。
- 同梱の Compose プロファイルでは、元のライブラリを読み取り専用でマウントします。
  表示用メタデータの上書きで、元ファイルの名前変更や書き換えは行いません。
- コンテンツ配信元のアカウントは不要で、オンライン配信元との連携も含みません。
  コアのカタログ機能と読書ワークフローは、不要な外部サービスに依存しません。
- 制限付きの実行時診断と、取り込み元ツリーとライブラリツリーを比較する、
  ソースコードのみで提供される読み取り専用インポート計画ツールが含まれます。

### 現在の対応範囲

| 入力 | Alpha 版公開コア |
| --- | --- |
| 画像フォルダー | 対応 |
| ZIP/CBZ 画像アーカイブ | 対応 |
| 1階層の入れ子 ZIP 画像アーカイブ | 対応 |
| 画像ベースの EPUB | Go ネイティブの ZIP リーダー経由で対応 |
| PDF（ZIP 内の PDF を含む） | 非搭載 |
| 7z | 非搭載 |
| MOBI 変換 | 非搭載 |
| オンライン配信元、ダウンロード、同期アダプター | 非搭載 |

非対応形式は非公開の補助ツールに暗黙に渡されず、意図的に拒否またはスキップされます。
完全な境界は
[`docs/architecture/public-core-boundary.md`](docs/architecture/public-core-boundary.md)
を参照してください。

<a id="5-minute-ghcr-trial"></a>

## GHCR で5分間試す

Docker と Compose、Linux/amd64 ホスト（または互換性のある amd64 Linux VM）、
および利用権限のある素材のみを含むフォルダーが必要です。

```sh
git clone https://github.com/zzzcws/bmanga-core.git
cd bmanga-core
cp config/compose.env.example .env
cp config/libraries.example.json config/libraries.json
```

Git で追跡されない `.env` ファイルを編集し、少なくとも次の値を設定します。

```dotenv
BMANGA_IMAGE=ghcr.io/zzzcws/bmanga-core:0.1.0-alpha.3
BMANGA_AUTH_USER=bmanga
BMANGA_AUTH_PASSWORD=<a-long-random-password>
BMANGA_SESSION_SECRET=<a-different-long-random-value>
BMANGA_LIBRARY_PATH=/absolute/path/to/your/authorized-library
```

固定された Alpha 版を取得し、明示的にスキャンを実行してからサービスを起動します。

```sh
docker compose --env-file .env --profile tools pull
docker compose --env-file .env --profile tools run --rm scan
docker compose --env-file .env up -d bmanga
```

<http://127.0.0.1:8765> を開き、`.env` の認証情報でサインインします。
最初のスキャンでカタログデータが `bmanga-data` ボリュームに書き込まれますが、
読み取り専用でマウントされた元のライブラリには書き込みません。
サービスを停止するには次を実行します。

```sh
docker compose --env-file .env down
```

Alpha 期間中は、意図的に `latest` タグを公開しません。固定バージョンを更新する前に
[リリースノート](https://github.com/zzzcws/bmanga-core/releases/tag/v0.1.0-alpha.3)
を確認してください。

## 早期テスターを募集中

すでに利用権限のあるローカルの漫画・コミックライブラリを管理している方から、
率直なフィードバックを募集しています。最初のテストは小規模で構いません。

1. Linux/amd64 上でのセットアップと最初のスキャンにかかる時間を計ります。
2. 1つ以上の対応入力形式を試します。
3. デスクトップまたはモバイルで、本棚のナビゲーション、読書進捗、
   ビューアーの動作を確認します。
4. 分かりにくかった点、遅かった点、正しく動作しなかった点をお知らせください。

[不具合を報告][bug-report]、[機能を提案][feature-request]、または
[ディスカッションを開始][discussions]してください。
著作権で保護されたメディア、認証情報、非公開のホスト名、ライブラリの絶対パス、
マスキングされていないログは添付しないでください。セキュリティに関する報告には、
[`SECURITY.md`](SECURITY.md) に記載された非公開チャネルを使用してください。

## クリーンなチェックアウトからビルド

ソースコードからのビルドには、Go 1.26.6 以降と Node.js 24 以降が必要です。
CI とコンテナービルドでは、レビュー済みの Node.js 24.19.0 ツールチェーンを固定して使用します。

```sh
node tools/build-web-assets.mjs --ci
go test ./...
go vet ./...
mkdir -p out
CGO_ENABLED=0 go build -buildvcs=false -mod=readonly -trimpath \
  -o out/bmanga ./cmd/bmanga-go
CGO_ENABLED=0 go build -buildvcs=false -mod=readonly -trimpath \
  -o out/bmanga-scan ./cmd/bmanga-scan
```

ローカルコンテナーは次のようにビルドします。

```sh
docker build -t bmanga:local .
```

フロントエンドのビルドはコミット済みの npm lockfile を使用し、Git で無視される出力を
`web/v2` に書き込みます。最終コンテナーには、2つの静的 Go バイナリ、生成済み Web アセット、
レビュー済みライセンスバンドルだけが含まれ、Python や Node.js/npm のランタイム、
文書・アーカイブ用の補助パッケージは含まれません。

## ローカル限定のワークフロー

- **表示メタデータの上書き**：タイトル、作者、シリーズラベル、言語の上書きを SQLite に保存し、
  元ファイルは変更しません。
  [`docs/features/metadata-overrides.md`](docs/features/metadata-overrides.md) を参照してください。
- **実行時診断**：パスや元のエラーテキストを公開せず、稼働時間、データベースの可用性、
  アプリケーションキャッシュの制限付き集計を提供します。
  [`docs/features/runtime-diagnostics.md`](docs/features/runtime-diagnostics.md) を参照してください。
- **読み取り専用インポート計画ツール**：明示的に選択した取り込み元ツリーとライブラリツリーを
  ハッシュで比較し、非公開の JSON レビュー計画を書き出します。適用、移動、上書き、隔離、
  削除の操作はありません。これはソースコード内のツールで、Alpha コンテナーには含まれません。
  [`docs/read-only-import-planner.md`](docs/read-only-import-planner.md) を参照してください。

## リポジトリ構成

- `cmd/bmanga-go` — サービスのエントリーポイントと認証境界。
- `cmd/bmanga-scan` — 明示的に実行する、入力元に依存しないカタログスキャナー。
- `cmd/bmanga-import-plan` — 境界を制限した、読み取り専用の取り込み元/ライブラリ比較ツール。
- `internal/prototype` — カタログ、ビューアー、レビュー、ローカル状態の API。
- `web-v2` — React/Vite インターフェースとテスト。
- `tools/build-web-assets.mjs` — 依存関係をロックした V2 本番用アセットビルダー。
- `Dockerfile` と `compose.yaml` — クリーンなチェックアウトから構築できる Linux/amd64 デプロイプロファイル。
- `docs/releasing.md` — プライバシー、サプライチェーン、リリース証跡のゲート。

## セキュリティ、コンテンツ、リリースの境界

bmanga-core には、インポート可能な書籍、単体の表紙画像、アカウント認証情報、
コンテンツ配信元のセッション、サンプルアーカイブは同梱されていません。
認証、DRM、アクセス制御、コンテンツ配信元の利用規約を回避するためのものではありません。
運用者は、利用許可のある素材のみを使用する責任を負います。

同梱の Compose プロファイルは HTTP を `127.0.0.1` にバインドし、元のライブラリを
読み取り専用でマウントし、Linux capabilities を破棄し、スクラッチイメージを
数値ユーザー `65532:65532` として実行します。これらはデプロイ上の制約であり、
あらゆる環境や信頼できないアーカイブの安全性を保証するものではありません。
リモートアクセスが必要な場合は、認証機能付きの TLS リバースプロキシを使用し、
`BMANGA_COOKIE_SECURE=1` を設定してください。

公開ソースと公開済み成果物には別々のゲートがあります。タグ付き Alpha イメージは
不変のコミットからビルドされ、検査とスモークテストを経て、イメージ SBOM、
GitHub build provenance（ビルド来歴）、キーレス署名とともに公開されます。創設メンテナーの
初期コンテンツ公開権限は
[`docs/first-party-rights.md`](docs/first-party-rights.md) に記録され、チェックイン済みの
サードパーティ対応表と技術レビュー記録は [`LICENSES/`](LICENSES/) にあります。
これらの記録は法的助言ではありません。

## プロジェクトリンク

- [コントリビュートガイド](CONTRIBUTING.md)
- [セキュリティポリシー](SECURITY.md)
- [サポート](SUPPORT.md)
- [行動規範](CODE_OF_CONDUCT.md)
- [メンテナーとガバナンス](MAINTAINERS.md)
- [変更履歴](CHANGELOG.md)
- [Apache-2.0 プロジェクトライセンス](LICENSE)
- [サードパーティ通知](THIRD_PARTY_NOTICES.md)
- [サードパーティライセンスバンドル](LICENSES/README.md)

[bug-report]: https://github.com/zzzcws/bmanga-core/issues/new?template=bug_report.yml
[feature-request]: https://github.com/zzzcws/bmanga-core/issues/new?template=feature_request.yml
[discussions]: https://github.com/zzzcws/bmanga-core/discussions
