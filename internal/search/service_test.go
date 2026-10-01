package search

import (
	"context"
	"errors"
	"testing"

	"github.com/o-ga09/gourmet-mcp/internal/jev"
	"github.com/o-ga09/gourmet-mcp/internal/places"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func candidates() []places.Place {
	return []places.Place{
		{ID: "a", Name: "A", GoogleMapsURI: "uri-a", PriceMinYen: 3000, PriceMaxYen: 4000, Rating: 4.1, Reviews: []string{"r1", "r2", "r3", "r4", "r5", "r6"}},
		{ID: "b", Name: "B", GoogleMapsURI: "uri-b", PriceMinYen: 8000, PriceMaxYen: 10000},
		{ID: "c", Name: "C", GoogleMapsURI: "uri-c"},
	}
}

// scoreByName は state 内の店名に応じたスコアを返す Judge を作る。
func scoreByName(scores map[string]float64, failFor ...string) *JudgeMock {
	return &JudgeMock{
		NoulFunc: func(ctx context.Context, state any, q jev.NoulQuestion) (float64, error) {
			name := state.(judgeState).Restaurant.Name
			for _, f := range failFor {
				if f == name {
					return 0, errors.New("jev failed")
				}
			}
			return scores[name], nil
		},
	}
}

func TestService_Search(t *testing.T) {
	tests := []struct {
		name       string
		query      Query
		places     []places.Place
		placesErr  error
		judge      *JudgeMock
		want       Result
		wantErr    bool
		wantQuery  places.SearchTextRequest
		wantJudged int
	}{
		{
			name:   "正常系: Jev のスコア降順に並べ limit 件返す",
			query:  Query{Area: "渋谷", Request: "静かな和食", OpenNow: true, Limit: 2},
			places: candidates(),
			judge:  scoreByName(map[string]float64{"A": 0.6, "B": 0.2, "C": 0.9}),
			want: Result{Restaurants: []Restaurant{
				{Name: "C", GoogleMapsURI: "uri-c", MatchScore: 0.9},
				{Name: "A", GoogleMapsURI: "uri-a", Rating: 4.1, PriceRange: "¥3,000〜¥4,000", MatchScore: 0.6},
			}},
			wantQuery:  places.SearchTextRequest{TextQuery: "渋谷 静かな和食", OpenNow: true, PageSize: 20},
			wantJudged: 3,
		},
		{
			name:   "正常系: 予算を超える店は Jev に渡さず除外する（価格不明は残す）",
			query:  Query{Area: "渋谷", Request: "和食", BudgetMaxYen: 5000},
			places: candidates(),
			judge:  scoreByName(map[string]float64{"A": 0.7, "B": 1.0, "C": 0.8}),
			want: Result{Restaurants: []Restaurant{
				{Name: "C", GoogleMapsURI: "uri-c", MatchScore: 0.8},
				{Name: "A", GoogleMapsURI: "uri-a", Rating: 4.1, PriceRange: "¥3,000〜¥4,000", MatchScore: 0.7},
			}},
			wantQuery:  places.SearchTextRequest{TextQuery: "渋谷 和食", PageSize: 20},
			wantJudged: 2,
		},
		{
			name:   "正常系: 最高スコアが閾値未満なら LowConfidence",
			query:  Query{Area: "渋谷", Request: "和食", Limit: 1},
			places: candidates(),
			judge:  scoreByName(map[string]float64{"A": 0.3, "B": 0.1, "C": 0.2}),
			want: Result{
				Restaurants:   []Restaurant{{Name: "A", GoogleMapsURI: "uri-a", Rating: 4.1, PriceRange: "¥3,000〜¥4,000", MatchScore: 0.3}},
				LowConfidence: true,
			},
			wantQuery:  places.SearchTextRequest{TextQuery: "渋谷 和食", PageSize: 20},
			wantJudged: 3,
		},
		{
			name:       "正常系: 候補 0 件なら LowConfidence で空を返す",
			query:      Query{Area: "渋谷", Request: "和食"},
			places:     []places.Place{},
			judge:      scoreByName(nil),
			want:       Result{Restaurants: []Restaurant{}, LowConfidence: true},
			wantQuery:  places.SearchTextRequest{TextQuery: "渋谷 和食", PageSize: 20},
			wantJudged: 0,
		},
		{
			name:   "正常系: 一部の Jev 呼び出しが失敗してもその店を除いて返す",
			query:  Query{Area: "渋谷", Request: "和食"},
			places: candidates(),
			judge:  scoreByName(map[string]float64{"A": 0.6, "B": 0.9, "C": 0.7}, "B"),
			want: Result{Restaurants: []Restaurant{
				{Name: "C", GoogleMapsURI: "uri-c", MatchScore: 0.7},
				{Name: "A", GoogleMapsURI: "uri-a", Rating: 4.1, PriceRange: "¥3,000〜¥4,000", MatchScore: 0.6},
			}},
			wantQuery:  places.SearchTextRequest{TextQuery: "渋谷 和食", PageSize: 20},
			wantJudged: 3,
		},
		{
			name:       "異常系: Jev 呼び出しがすべて失敗したらエラー",
			query:      Query{Area: "渋谷", Request: "和食"},
			places:     candidates(),
			judge:      scoreByName(nil, "A", "B", "C"),
			wantErr:    true,
			wantQuery:  places.SearchTextRequest{TextQuery: "渋谷 和食", PageSize: 20},
			wantJudged: 3,
		},
		{
			name:      "異常系: Places がエラーならエラー",
			query:     Query{Area: "渋谷", Request: "和食"},
			placesErr: errors.New("places failed"),
			judge:     scoreByName(nil),
			wantErr:   true,
			wantQuery: places.SearchTextRequest{TextQuery: "渋谷 和食", PageSize: 20},
		},
		{
			name:    "異常系: area が空ならエラー",
			query:   Query{Request: "和食"},
			judge:   scoreByName(nil),
			wantErr: true,
		},
		{
			name:    "異常系: request が空ならエラー",
			query:   Query{Area: "渋谷"},
			judge:   scoreByName(nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := &PlaceSearcherMock{
				SearchTextFunc: func(ctx context.Context, r places.SearchTextRequest) ([]places.Place, error) {
					return tt.places, tt.placesErr
				},
			}
			svc := NewService(ps, tt.judge)

			got, err := svc.Search(context.Background(), tt.query)

			if tt.wantQuery.TextQuery != "" {
				require.Len(t, ps.SearchTextCalls(), 1)
				assert.Equal(t, tt.wantQuery, ps.SearchTextCalls()[0].R)
			} else {
				assert.Empty(t, ps.SearchTextCalls())
			}
			assert.Len(t, tt.judge.NoulCalls(), tt.wantJudged)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestService_Search_JudgeState(t *testing.T) {
	ps := &PlaceSearcherMock{
		SearchTextFunc: func(ctx context.Context, r places.SearchTextRequest) ([]places.Place, error) {
			return candidates()[:1], nil
		},
	}
	judge := scoreByName(map[string]float64{"A": 0.5})
	svc := NewService(ps, judge)

	_, err := svc.Search(context.Background(), Query{Area: "渋谷", Request: "静かな和食", BudgetMaxYen: 5000})
	require.NoError(t, err)

	require.Len(t, judge.NoulCalls(), 1)
	call := judge.NoulCalls()[0]
	assert.Equal(t, judgeState{
		Request:      "静かな和食",
		BudgetMaxYen: 5000,
		Restaurant: judgeRestaurant{
			Name:       "A",
			Rating:     4.1,
			PriceRange: "¥3,000〜¥4,000",
			Reviews:    []string{"r1", "r2", "r3", "r4", "r5"},
		},
	}, call.State)
	assert.NotEmpty(t, call.Q.Instructions)
	assert.NotEmpty(t, call.Q.True)
	assert.NotEmpty(t, call.Q.False)
}

func TestFormatPriceRange(t *testing.T) {
	tests := []struct {
		name     string
		min, max int
		want     string
	}{
		{name: "下限と上限", min: 3000, max: 4000, want: "¥3,000〜¥4,000"},
		{name: "下限のみ", min: 10000, want: "¥10,000〜"},
		{name: "上限のみ", max: 1000, want: "〜¥1,000"},
		{name: "不明", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatPriceRange(tt.min, tt.max))
		})
	}
}
