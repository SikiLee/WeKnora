package config

import (
	"strings"
	"testing"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFeedbackConfigDefaultsMergePartialYAMLSection(t *testing.T) {
	v := viper.New()
	registerFeedbackConfigDefaults(v.SetDefault)
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("feedback:\n  high_rate_threshold: 0.9\n")); err != nil {
		t.Fatalf("read partial config: %v", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "yaml"
	}); err != nil {
		t.Fatalf("decode partial config: %v", err)
	}
	if cfg.Feedback == nil ||
		cfg.Feedback.HighRateThreshold != 0.9 ||
		cfg.Feedback.LowRateThreshold != 0.5 ||
		cfg.Feedback.OptimizationThreshold != 0.2 ||
		cfg.Feedback.HighRecallWeight != 1.2 ||
		cfg.Feedback.NormalRecallWeight != 1.0 ||
		cfg.Feedback.LowRecallWeight != 0.8 {
		t.Fatalf("partial feedback config did not inherit defaults: %+v", cfg.Feedback)
	}
	if err := ValidateConfig(&cfg); err != nil {
		t.Fatalf("partial feedback config failed validation: %v", err)
	}
}

func TestApplyFeedbackDefaultsAndEnvOverrides(t *testing.T) {
	t.Run("defaults when section is absent", func(t *testing.T) {
		cfg := &Config{}

		applyFeedbackDefaultsAndEnvOverrides(cfg)

		if cfg.Feedback == nil {
			t.Fatal("Feedback config was not initialized")
		}
		if cfg.Feedback.HighRateThreshold != 0.8 ||
			cfg.Feedback.LowRateThreshold != 0.5 ||
			cfg.Feedback.OptimizationThreshold != 0.2 ||
			cfg.Feedback.HighRecallWeight != 1.2 ||
			cfg.Feedback.NormalRecallWeight != 1.0 ||
			cfg.Feedback.LowRecallWeight != 0.8 {
			t.Fatalf("unexpected feedback defaults: %+v", cfg.Feedback)
		}
	})

	t.Run("environment overrides yaml values", func(t *testing.T) {
		t.Setenv("WEKNORA_FEEDBACK_HIGH_RATE_THRESHOLD", "0.9")
		t.Setenv("WEKNORA_FEEDBACK_LOW_RATE_THRESHOLD", "0.4")
		t.Setenv("WEKNORA_FEEDBACK_OPTIMIZATION_THRESHOLD", "0.1")
		t.Setenv("WEKNORA_FEEDBACK_HIGH_RECALL_WEIGHT", "1.3")
		t.Setenv("WEKNORA_FEEDBACK_NORMAL_RECALL_WEIGHT", "1.1")
		t.Setenv("WEKNORA_FEEDBACK_LOW_RECALL_WEIGHT", "0.7")

		cfg := &Config{Feedback: types.DefaultChunkFeedbackConfig()}
		applyFeedbackDefaultsAndEnvOverrides(cfg)

		if cfg.Feedback.HighRateThreshold != 0.9 ||
			cfg.Feedback.LowRateThreshold != 0.4 ||
			cfg.Feedback.OptimizationThreshold != 0.1 ||
			cfg.Feedback.HighRecallWeight != 1.3 ||
			cfg.Feedback.NormalRecallWeight != 1.1 ||
			cfg.Feedback.LowRecallWeight != 0.7 {
			t.Fatalf("unexpected env overrides: %+v", cfg.Feedback)
		}
	})
}

func TestValidateConfigFeedback(t *testing.T) {
	cfg := &Config{}
	applyFeedbackDefaultsAndEnvOverrides(cfg)

	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig rejected defaults: %v", err)
	}

	cfg.Feedback.LowRateThreshold = 0.9
	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("ValidateConfig unexpectedly accepted invalid feedback thresholds")
	}
	if !strings.Contains(err.Error(), "feedback thresholds") {
		t.Fatalf("ValidateConfig error = %q, want feedback thresholds", err.Error())
	}
}

func TestValidateConfigFeedbackRejectsNonFiniteEnvOverride(t *testing.T) {
	t.Setenv("WEKNORA_FEEDBACK_HIGH_RECALL_WEIGHT", "+Inf")

	cfg := &Config{}
	applyFeedbackDefaultsAndEnvOverrides(cfg)

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("ValidateConfig unexpectedly accepted non-finite feedback env override")
	}
	if !strings.Contains(err.Error(), "must be finite") {
		t.Fatalf("ValidateConfig error = %q, want finite validation", err.Error())
	}
}
