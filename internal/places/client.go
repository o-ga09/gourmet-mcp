// Package places は Google Places API (New) の Text Search を呼び出すクライアント。
package places

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const defaultBaseURL = "https://places.googleapis.com"

// fieldMask は取得するフィールド。reviews を含むため Enterprise + Atmosphere SKU で課金される
// （課金はリクエスト単位）。
const fieldMask = "places.id,places.displayName,places.formattedAddress,places.rating," +
	"places.userRatingCount,places.priceRange,places.editorialSummary,places.reviews,places.googleMapsUri"

type SearchTextRequest struct {
	TextQuery string
	OpenNow   bool
	PageSize  int
}

type Place struct {
	ID               string
	Name             string
	Address          string
	Rating           float64
	UserRatingCount  int
	GoogleMapsURI    string
	PriceMinYen      int
	PriceMaxYen      int
	EditorialSummary string
	Reviews          []string
}

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type searchTextBody struct {
	TextQuery    string `json:"textQuery"`
	IncludedType string `json:"includedType"`
	LanguageCode string `json:"languageCode"`
	RegionCode   string `json:"regionCode"`
	OpenNow      bool   `json:"openNow,omitempty"`
	PageSize     int    `json:"pageSize,omitempty"`
}

type localizedText struct {
	Text string `json:"text"`
}

type money struct {
	CurrencyCode string `json:"currencyCode"`
	Units        string `json:"units"`
}

type apiPlace struct {
	ID               string        `json:"id"`
	DisplayName      localizedText `json:"displayName"`
	FormattedAddress string        `json:"formattedAddress"`
	Rating           float64       `json:"rating"`
	UserRatingCount  int           `json:"userRatingCount"`
	GoogleMapsURI    string        `json:"googleMapsUri"`
	PriceRange       *struct {
		StartPrice *money `json:"startPrice"`
		EndPrice   *money `json:"endPrice"`
	} `json:"priceRange"`
	EditorialSummary localizedText `json:"editorialSummary"`
	Reviews          []struct {
		Text localizedText `json:"text"`
	} `json:"reviews"`
}

func (c *Client) SearchText(ctx context.Context, r SearchTextRequest) ([]Place, error) {
	payload, err := json.Marshal(searchTextBody{
		TextQuery:    r.TextQuery,
		IncludedType: "restaurant",
		LanguageCode: "ja",
		RegionCode:   "JP",
		OpenNow:      r.OpenNow,
		PageSize:     r.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal places request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/places:searchText", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build places request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", c.apiKey)
	req.Header.Set("X-Goog-FieldMask", fieldMask)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call places: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("places: status %d: %s", resp.StatusCode, body)
	}

	var rb struct {
		Places []apiPlace `json:"places"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		return nil, fmt.Errorf("decode places response: %w", err)
	}

	out := make([]Place, 0, len(rb.Places))
	for _, p := range rb.Places {
		out = append(out, toPlace(p))
	}
	return out, nil
}

func toPlace(p apiPlace) Place {
	pl := Place{
		ID:               p.ID,
		Name:             p.DisplayName.Text,
		Address:          p.FormattedAddress,
		Rating:           p.Rating,
		UserRatingCount:  p.UserRatingCount,
		GoogleMapsURI:    p.GoogleMapsURI,
		EditorialSummary: p.EditorialSummary.Text,
	}
	if p.PriceRange != nil {
		pl.PriceMinYen = yen(p.PriceRange.StartPrice)
		pl.PriceMaxYen = yen(p.PriceRange.EndPrice)
	}
	for _, rv := range p.Reviews {
		if rv.Text.Text != "" {
			pl.Reviews = append(pl.Reviews, rv.Text.Text)
		}
	}
	return pl
}

// yen は JPY の金額を整数で返す。JPY 以外や未設定は 0。
func yen(m *money) int {
	if m == nil || m.CurrencyCode != "JPY" {
		return 0
	}
	n, err := strconv.Atoi(m.Units)
	if err != nil {
		return 0
	}
	return n
}
