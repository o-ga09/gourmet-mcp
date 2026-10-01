# gourmet-mcp

Hermes Agent（LINE / Slack）から呼び出す飲食店検索 MCP サーバー。

- 候補取得: Google Places API (New) Text Search
- 要望への適合度判定・並べ替え: [Jev](https://typesafe.ai/)（TypeSafe AI）の `noul`

## ツール

### `search_restaurants`

| 引数 | 必須 | 説明 |
|---|---|---|
| `area` | ✅ | 駅名・地名（例: 渋谷駅） |
| `request` | ✅ | 要望の自然文（例: 静かで落ち着いた和食） |
| `budget_max_yen` | | 1 人あたりの予算上限（円） |
| `open_now` | | 営業中の店に限定 |
| `limit` | | 返す件数（既定 5、最大 10） |

処理の流れ:

1. `{area} {request}` で Places Text Search（最大 20 件、口コミ込み）
2. 下限価格が予算を超える店を除外（価格不明は残す）
3. 各候補について「要望を満たすか」を Jev で並列判定（失敗した候補は除外）
4. スコア降順で `limit` 件を返す。最高スコアが 0.5 未満なら `low_confidence: true`

返却する各店には Google Maps Platform の規約に従い `google_maps_uri` を含める。

## 起動

```bash
export GOOGLE_PLACES_API_KEY=...   # Places API のみに制限したキー
export TYPESAFE_API_KEY=...
go run ./cmd/server               # :8080/mcp（Streamable HTTP, stateless）, /healthz
```

## 開発

```bash
go test -race ./...
golangci-lint run ./...
go generate ./...   # moq でモック再生成
```

ローカルの Go が 1.27 の場合、moq / golangci-lint が標準ライブラリを読めないため
`GOTOOLCHAIN=go1.26.0` を付けて実行する。

## デプロイ

main への push で `asia-northeast1-docker.pkg.dev/mh-api-389212/gourmet/gourmet-mcp:<sha>` を
ビルド・push し、o-ga09/infra の `manifests/gourmet-mcp/mcp-deployment.yaml` の image を更新する
（コンテナ名 `gourmet-mcp`）。

必要な GitHub Secrets: `GCP_PROJECT_NUMBER`, `SERVICE_ACCOUNT`, `GH_PAT`
