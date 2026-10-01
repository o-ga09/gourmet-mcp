package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/o-ga09/gourmet-mcp/internal/search"
)

type searcherFunc func(ctx context.Context, q search.Query) (search.Result, error)

func (f searcherFunc) Search(ctx context.Context, q search.Query) (search.Result, error) {
	return f(ctx, q)
}

func connect(t *testing.T, s Searcher) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	_, err := New(s).Connect(ctx, st, nil)
	require.NoError(t, err)
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestSearchRestaurantsTool(t *testing.T) {
	result := search.Result{Restaurants: []search.Restaurant{
		{Name: "和食 しずか", GoogleMapsURI: "https://maps.google.com/?cid=1", MatchScore: 0.9},
	}}

	tests := []struct {
		name      string
		args      map[string]any
		searchErr error
		wantQuery search.Query
		wantErr   bool
	}{
		{
			name: "正常系: 引数を Query に変換し結果を構造化して返す",
			args: map[string]any{
				"area":           "渋谷",
				"request":        "静かな和食",
				"budget_max_yen": 4000,
				"open_now":       true,
				"limit":          3,
			},
			wantQuery: search.Query{Area: "渋谷", Request: "静かな和食", BudgetMaxYen: 4000, OpenNow: true, Limit: 3},
		},
		{
			name:      "正常系: 任意引数は省略できる",
			args:      map[string]any{"area": "新宿", "request": "ラーメン"},
			wantQuery: search.Query{Area: "新宿", Request: "ラーメン"},
		},
		{
			name:      "異常系: 検索エラーはツールエラーとして返す",
			args:      map[string]any{"area": "新宿", "request": "ラーメン"},
			searchErr: errors.New("places failed"),
			wantQuery: search.Query{Area: "新宿", Request: "ラーメン"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery search.Query
			cs := connect(t, searcherFunc(func(ctx context.Context, q search.Query) (search.Result, error) {
				gotQuery = q
				return result, tt.searchErr
			}))

			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "search_restaurants",
				Arguments: tt.args,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, gotQuery)

			if tt.wantErr {
				assert.True(t, res.IsError)
				return
			}
			require.False(t, res.IsError)

			raw, err := json.Marshal(res.StructuredContent)
			require.NoError(t, err)
			var got search.Result
			require.NoError(t, json.Unmarshal(raw, &got))
			assert.Equal(t, result, got)
		})
	}
}

func TestSearchRestaurantsTool_RequiresAreaAndRequest(t *testing.T) {
	cs := connect(t, searcherFunc(func(ctx context.Context, q search.Query) (search.Result, error) {
		t.Fatal("search must not be called")
		return search.Result{}, nil
	}))

	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, res.Tools, 1)

	raw, err := json.Marshal(res.Tools[0].InputSchema)
	require.NoError(t, err)
	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	assert.ElementsMatch(t, []string{"area", "request"}, schema.Required)
}
