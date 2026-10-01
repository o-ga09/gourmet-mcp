package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Noul(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []int
		response  string
		want      float64
		wantErr   bool
		wantCalls int32
	}{
		{
			name:      "正常系: noul の値を返す",
			statuses:  []int{http.StatusOK},
			response:  `{"model":"jev-1.13.0","answers":{"match":{"type":"noul","noul":0.87}}}`,
			want:      0.87,
			wantCalls: 1,
		},
		{
			name:      "正常系: 429 の後にリトライして成功する",
			statuses:  []int{http.StatusTooManyRequests, http.StatusOK},
			response:  `{"answers":{"match":{"type":"noul","noul":0.4}}}`,
			want:      0.4,
			wantCalls: 2,
		},
		{
			name:      "正常系: 529 の後にリトライして成功する",
			statuses:  []int{529, http.StatusOK},
			response:  `{"answers":{"match":{"type":"noul","noul":0.1}}}`,
			want:      0.1,
			wantCalls: 2,
		},
		{
			name:      "異常系: リトライ上限を超えるとエラー",
			statuses:  []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusTooManyRequests},
			wantErr:   true,
			wantCalls: 3,
		},
		{
			name:      "異常系: 401 はリトライせずエラー",
			statuses:  []int{http.StatusUnauthorized},
			wantErr:   true,
			wantCalls: 1,
		},
		{
			name:      "異常系: 回答に質問キーが含まれない",
			statuses:  []int{http.StatusOK},
			response:  `{"answers":{}}`,
			wantErr:   true,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/systemone", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "jev-latest", body["model"])
				assert.Equal(t, map[string]any{"request": "静かな和食"}, body["state"])
				q := body["questions"].(map[string]any)["match"].(map[string]any)
				assert.Equal(t, "noul", q["type"])
				assert.Equal(t, "要望を満たすか", q["instructions"])
				assert.Equal(t, map[string]any{"true": "満たす", "false": "満たさない"}, q["criteria"])

				w.WriteHeader(tt.statuses[n-1])
				_, _ = w.Write([]byte(tt.response))
			}))
			defer srv.Close()

			c := NewClient("test-key", WithBaseURL(srv.URL), WithMaxAttempts(3), WithBaseBackoff(time.Millisecond))
			got, err := c.Noul(context.Background(), map[string]any{"request": "静かな和食"}, NoulQuestion{
				Instructions: "要望を満たすか",
				True:         "満たす",
				False:        "満たさない",
			})

			assert.Equal(t, tt.wantCalls, calls.Load())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}
