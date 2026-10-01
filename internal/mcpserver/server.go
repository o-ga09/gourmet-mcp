// Package mcpserver は飲食店検索を MCP ツールとして公開する。
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/o-ga09/gourmet-mcp/internal/search"
)

type Searcher interface {
	Search(ctx context.Context, q search.Query) (search.Result, error)
}

type searchInput struct {
	Area         string `json:"area" jsonschema:"検索するエリア。駅名・地名など（例: 渋谷駅）"`
	Request      string `json:"request" jsonschema:"ユーザーの要望を自然文で（例: 静かで落ち着いた和食、個室あり）"`
	BudgetMaxYen int    `json:"budget_max_yen,omitempty" jsonschema:"1人あたりの予算上限（円）。指定がなければ省略"`
	OpenNow      bool   `json:"open_now,omitempty" jsonschema:"現在営業中の店に限定するか"`
	Limit        int    `json:"limit,omitempty" jsonschema:"返す件数（既定 5、最大 10）"`
}

const toolDescription = "Google マップの飲食店を検索し、ユーザーの要望に合う順に並べて返す。" +
	"結果をユーザーに伝えるときは各店の google_maps_uri を必ず添えること。" +
	"low_confidence が true の場合は要望に十分合う店が見つかっていないので、条件を緩めるか確認すること。"

func New(s Searcher) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "gourmet-mcp", Version: "v0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_restaurants",
		Description: toolDescription,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, search.Result, error) {
		res, err := s.Search(ctx, search.Query{
			Area:         in.Area,
			Request:      in.Request,
			BudgetMaxYen: in.BudgetMaxYen,
			OpenNow:      in.OpenNow,
			Limit:        in.Limit,
		})
		if err != nil {
			return nil, search.Result{}, err
		}
		return nil, res, nil
	})
	return server
}
