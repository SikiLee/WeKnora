package types

import (
	"encoding/json"
	"math"
	"testing"
)

func TestCalculateChunkFeedback(t *testing.T) {
	cfg := DefaultChunkFeedbackConfig()

	cases := []struct {
		name             string
		likes            int64
		dislikes         int64
		wantRate         *float64
		wantWeight       float64
		wantOptimization bool
	}{
		{
			name:       "no feedback keeps normal weight",
			likes:      0,
			dislikes:   0,
			wantRate:   nil,
			wantWeight: cfg.NormalRecallWeight,
		},
		{
			name:       "high positive rate boosts recall",
			likes:      8,
			dislikes:   2,
			wantRate:   ptrFloat(0.8),
			wantWeight: cfg.HighRecallWeight,
		},
		{
			name:       "low threshold boundary keeps normal recall",
			likes:      1,
			dislikes:   1,
			wantRate:   ptrFloat(0.5),
			wantWeight: cfg.NormalRecallWeight,
		},
		{
			name:       "normal positive rate keeps normal recall",
			likes:      3,
			dislikes:   2,
			wantRate:   ptrFloat(0.6),
			wantWeight: cfg.NormalRecallWeight,
		},
		{
			name:             "low positive rate lowers recall",
			likes:            1,
			dislikes:         4,
			wantRate:         ptrFloat(0.2),
			wantWeight:       cfg.LowRecallWeight,
			wantOptimization: true,
		},
		{
			name:       "just above optimization threshold does not need optimization",
			likes:      21,
			dislikes:   79,
			wantRate:   ptrFloat(0.21),
			wantWeight: cfg.LowRecallWeight,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRate, gotWeight, gotOptimization := CalculateChunkFeedback(tc.likes, tc.dislikes, cfg)
			if tc.wantRate == nil {
				if gotRate != nil {
					t.Fatalf("positiveRate = %v, want nil", *gotRate)
				}
			} else if gotRate == nil || *gotRate != *tc.wantRate {
				if gotRate == nil {
					t.Fatalf("positiveRate = nil, want %v", *tc.wantRate)
				}
				t.Fatalf("positiveRate = %v, want %v", *gotRate, *tc.wantRate)
			}
			if gotWeight != tc.wantWeight {
				t.Fatalf("recallWeight = %v, want %v", gotWeight, tc.wantWeight)
			}
			if gotOptimization != tc.wantOptimization {
				t.Fatalf("needsOptimization = %v, want %v", gotOptimization, tc.wantOptimization)
			}
		})
	}
}

func TestChunkFeedbackConfigValidate(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		if err := DefaultChunkFeedbackConfig().Validate(); err != nil {
			t.Fatalf("default config invalid: %v", err)
		}
	})

	t.Run("rejects invalid threshold order", func(t *testing.T) {
		cfg := DefaultChunkFeedbackConfig()
		cfg.LowRateThreshold = 0.9
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate unexpectedly accepted invalid threshold order")
		}
	})

	t.Run("rejects non-positive weight", func(t *testing.T) {
		cfg := DefaultChunkFeedbackConfig()
		cfg.LowRecallWeight = 0
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate unexpectedly accepted zero recall weight")
		}
	})

	t.Run("rejects non-finite thresholds and weights", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*ChunkFeedbackConfig)
		}{
			{
				name: "nan threshold",
				mutate: func(cfg *ChunkFeedbackConfig) {
					cfg.HighRateThreshold = math.NaN()
				},
			},
			{
				name: "positive infinity threshold",
				mutate: func(cfg *ChunkFeedbackConfig) {
					cfg.LowRateThreshold = math.Inf(1)
				},
			},
			{
				name: "negative infinity weight",
				mutate: func(cfg *ChunkFeedbackConfig) {
					cfg.NormalRecallWeight = math.Inf(-1)
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := DefaultChunkFeedbackConfig()
				tc.mutate(cfg)
				if err := cfg.Validate(); err == nil {
					t.Fatal("Validate unexpectedly accepted non-finite value")
				}
			})
		}
	})
}

func TestChunkFeedbackFieldsAreNotSerialized(t *testing.T) {
	rate := 0.25
	chunk := Chunk{
		ID:                "chunk-1",
		Content:           "visible content",
		LikeCount:         1,
		DislikeCount:      3,
		PositiveRate:      &rate,
		RecallWeight:      0.8,
		NeedsOptimization: true,
	}

	raw, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if got["id"] != chunk.ID || got["content"] != chunk.Content {
		t.Fatalf("expected normal chunk fields to serialize, got %s", raw)
	}

	hiddenFields := []string{
		"like_count",
		"dislike_count",
		"positive_rate",
		"recall_weight",
		"needs_optimization",
		"feedback_reset_at",
		"feedback_updated_at",
	}
	for _, field := range hiddenFields {
		if _, ok := got[field]; ok {
			t.Fatalf("feedback governance field %q leaked in JSON: %s", field, raw)
		}
	}
}

func ptrFloat(v float64) *float64 {
	return &v
}
