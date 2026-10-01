// Package search は Google Places で候補を集め、Jev で要望への適合度を判定して並べ替える。
package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/o-ga09/gourmet-mcp/internal/jev"
	"github.com/o-ga09/gourmet-mcp/internal/places"
)

//go:generate go run github.com/matryer/moq@latest -out mock_test.go . PlaceSearcher Judge

type PlaceSearcher interface {
	SearchText(ctx context.Context, r places.SearchTextRequest) ([]places.Place, error)
}

type Judge interface {
	Noul(ctx context.Context, state any, q jev.NoulQuestion) (float64, error)
}

const (
	candidatePageSize = 20
	defaultLimit      = 5
	maxLimit          = 10
	maxReviews        = 5
	judgeConcurrency  = 8

	// lowConfidenceThreshold を最高スコアが下回る場合、要望に合う店が見つからなかったとみなす。
	lowConfidenceThreshold = 0.5
)

var matchQuestion = jev.NoulQuestion{
	Instructions: "この飲食店はユーザーの要望（予算が指定されていれば予算も含む）を満たしていますか？店の評価・価格帯・紹介文・口コミから判断してください。",
	True:         "店の情報や口コミから、要望の大部分を満たしていると判断できる",
	False:        "要望と食い違う、または要望を満たす根拠がない",
}

type Query struct {
	Area         string
	Request      string
	BudgetMaxYen int
	OpenNow      bool
	Limit        int
}

type Restaurant struct {
	Name            string  `json:"name"`
	Rating          float64 `json:"rating,omitempty"`
	UserRatingCount int     `json:"user_rating_count,omitempty"`
	PriceRange      string  `json:"price_range,omitempty"`
	Address         string  `json:"address,omitempty"`
	Summary         string  `json:"summary,omitempty"`
	GoogleMapsURI   string  `json:"google_maps_uri"`
	MatchScore      float64 `json:"match_score"`
}

type Result struct {
	Restaurants []Restaurant `json:"restaurants"`
	// LowConfidence は要望に十分合う店が見つからなかったことを示す。条件を緩める提案に使う。
	LowConfidence bool `json:"low_confidence"`
}

// judgeState は Jev に渡す state。
type judgeState struct {
	Request      string          `json:"request"`
	BudgetMaxYen int             `json:"budget_max_yen,omitempty"`
	Restaurant   judgeRestaurant `json:"restaurant"`
}

type judgeRestaurant struct {
	Name             string   `json:"name"`
	Rating           float64  `json:"rating,omitempty"`
	PriceRange       string   `json:"price_range,omitempty"`
	EditorialSummary string   `json:"editorial_summary,omitempty"`
	Reviews          []string `json:"reviews,omitempty"`
}

type Service struct {
	places PlaceSearcher
	judge  Judge
}

func NewService(p PlaceSearcher, j Judge) *Service {
	return &Service{places: p, judge: j}
}

func (s *Service) Search(ctx context.Context, q Query) (Result, error) {
	if strings.TrimSpace(q.Area) == "" {
		return Result{}, errors.New("area is required")
	}
	if strings.TrimSpace(q.Request) == "" {
		return Result{}, errors.New("request is required")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)

	found, err := s.places.SearchText(ctx, places.SearchTextRequest{
		TextQuery: q.Area + " " + q.Request,
		OpenNow:   q.OpenNow,
		PageSize:  candidatePageSize,
	})
	if err != nil {
		return Result{}, fmt.Errorf("search places: %w", err)
	}

	cands := withinBudget(found, q.BudgetMaxYen)
	if len(cands) == 0 {
		return Result{Restaurants: []Restaurant{}, LowConfidence: true}, nil
	}

	scored, err := s.judgeAll(ctx, q, cands)
	if err != nil {
		return Result{}, err
	}

	sort.SliceStable(scored, func(i, j int) bool { return scored[i].MatchScore > scored[j].MatchScore })
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return Result{
		Restaurants:   scored,
		LowConfidence: scored[0].MatchScore < lowConfidenceThreshold,
	}, nil
}

// judgeAll は候補ごとに Jev を並列で呼ぶ。失敗した候補は除外し、全件失敗ならエラーを返す。
func (s *Service) judgeAll(ctx context.Context, q Query, cands []places.Place) ([]Restaurant, error) {
	scores := make([]float64, len(cands))
	errs := make([]error, len(cands))

	var g errgroup.Group
	g.SetLimit(judgeConcurrency)
	for i, p := range cands {
		g.Go(func() error {
			scores[i], errs[i] = s.judge.Noul(ctx, judgeState{
				Request:      q.Request,
				BudgetMaxYen: q.BudgetMaxYen,
				Restaurant: judgeRestaurant{
					Name:             p.Name,
					Rating:           p.Rating,
					PriceRange:       formatPriceRange(p.PriceMinYen, p.PriceMaxYen),
					EditorialSummary: p.EditorialSummary,
					Reviews:          p.Reviews[:min(len(p.Reviews), maxReviews)],
				},
			}, matchQuestion)
			return nil
		})
	}
	_ = g.Wait()

	out := make([]Restaurant, 0, len(cands))
	for i, p := range cands {
		if errs[i] != nil {
			slog.WarnContext(ctx, "jev judge failed", "place_id", p.ID, "error", errs[i])
			continue
		}
		out = append(out, Restaurant{
			Name:            p.Name,
			Rating:          p.Rating,
			UserRatingCount: p.UserRatingCount,
			PriceRange:      formatPriceRange(p.PriceMinYen, p.PriceMaxYen),
			Address:         p.Address,
			Summary:         p.EditorialSummary,
			GoogleMapsURI:   p.GoogleMapsURI,
			MatchScore:      scores[i],
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("judge restaurants: all %d calls failed: %w", len(cands), errors.Join(errs...))
	}
	return out, nil
}

// withinBudget は下限価格が予算を超える店を除外する。価格不明の店は残す。
func withinBudget(ps []places.Place, budget int) []places.Place {
	if budget <= 0 {
		return ps
	}
	out := make([]places.Place, 0, len(ps))
	for _, p := range ps {
		if p.PriceMinYen > budget {
			continue
		}
		out = append(out, p)
	}
	return out
}

func formatPriceRange(minYen, maxYen int) string {
	switch {
	case minYen > 0 && maxYen > 0:
		return formatYen(minYen) + "〜" + formatYen(maxYen)
	case minYen > 0:
		return formatYen(minYen) + "〜"
	case maxYen > 0:
		return "〜" + formatYen(maxYen)
	default:
		return ""
	}
}

func formatYen(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return "¥" + b.String()
}
