package places

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleResponse = `{
  "places": [
    {
      "id": "abc",
      "displayName": {"text": "和食 しずか", "languageCode": "ja"},
      "formattedAddress": "東京都渋谷区1-2-3",
      "rating": 4.3,
      "userRatingCount": 120,
      "googleMapsUri": "https://maps.google.com/?cid=1",
      "priceRange": {
        "startPrice": {"currencyCode": "JPY", "units": "3000"},
        "endPrice": {"currencyCode": "JPY", "units": "4000"}
      },
      "editorialSummary": {"text": "落ち着いた雰囲気の和食店"},
      "reviews": [
        {"rating": 5, "text": {"text": "静かで落ち着く"}},
        {"rating": 4, "text": {"text": "料理が丁寧"}}
      ]
    },
    {
      "id": "def",
      "displayName": {"text": "居酒屋 にぎやか"},
      "formattedAddress": "東京都渋谷区4-5-6",
      "googleMapsUri": "https://maps.google.com/?cid=2",
      "priceRange": {"startPrice": {"currencyCode": "JPY", "units": "2000"}}
    }
  ]
}`

func TestClient_SearchText(t *testing.T) {
	tests := []struct {
		name     string
		req      SearchTextRequest
		status   int
		response string
		wantBody map[string]any
		want     []Place
		wantErr  bool
	}{
		{
			name:     "正常系: 店舗一覧を変換して返す",
			req:      SearchTextRequest{TextQuery: "渋谷 静かな和食", OpenNow: true, PageSize: 20},
			status:   http.StatusOK,
			response: sampleResponse,
			wantBody: map[string]any{
				"textQuery":    "渋谷 静かな和食",
				"includedType": "restaurant",
				"languageCode": "ja",
				"regionCode":   "JP",
				"openNow":      true,
				"pageSize":     float64(20),
			},
			want: []Place{
				{
					ID:               "abc",
					Name:             "和食 しずか",
					Address:          "東京都渋谷区1-2-3",
					Rating:           4.3,
					UserRatingCount:  120,
					GoogleMapsURI:    "https://maps.google.com/?cid=1",
					PriceMinYen:      3000,
					PriceMaxYen:      4000,
					EditorialSummary: "落ち着いた雰囲気の和食店",
					Reviews:          []string{"静かで落ち着く", "料理が丁寧"},
				},
				{
					ID:            "def",
					Name:          "居酒屋 にぎやか",
					Address:       "東京都渋谷区4-5-6",
					GoogleMapsURI: "https://maps.google.com/?cid=2",
					PriceMinYen:   2000,
				},
			},
		},
		{
			name:     "正常系: openNow が false なら送らない",
			req:      SearchTextRequest{TextQuery: "新宿 ラーメン"},
			status:   http.StatusOK,
			response: `{}`,
			wantBody: map[string]any{
				"textQuery":    "新宿 ラーメン",
				"includedType": "restaurant",
				"languageCode": "ja",
				"regionCode":   "JP",
			},
			want: []Place{},
		},
		{
			name:     "異常系: 403 はエラー",
			req:      SearchTextRequest{TextQuery: "x"},
			status:   http.StatusForbidden,
			response: `{"error":{"message":"denied"}}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/places:searchText", r.URL.Path)
				assert.Equal(t, "test-key", r.Header.Get("X-Goog-Api-Key"))
				assert.Equal(t, fieldMask, r.Header.Get("X-Goog-FieldMask"))

				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				if tt.wantBody != nil {
					assert.Equal(t, tt.wantBody, body)
				}

				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer srv.Close()

			c := NewClient("test-key", WithBaseURL(srv.URL))
			got, err := c.SearchText(context.Background(), tt.req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
