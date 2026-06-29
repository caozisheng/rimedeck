package daemon

import (
	"encoding/json"
	"log/slog"
	"math"
	"time"
)

type agentExecutionRuntimeConfig struct {
	Execution struct {
		TimeoutMinutes *float64 `json:"timeout_minutes"`
	} `json:"execution"`
}

func decodeAgentExecutionTimeout(raw json.RawMessage, fallback time.Duration, logger *slog.Logger) time.Duration {
	if len(raw) == 0 {
		return fallback
	}
	var cfg agentExecutionRuntimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		if logger != nil {
			logger.Warn("agent runtime_config: execution config parse failed; using default timeout", "error", err)
		}
		return fallback
	}
	if cfg.Execution.TimeoutMinutes == nil {
		return fallback
	}
	minutes := *cfg.Execution.TimeoutMinutes
	if !isValidExecutionTimeoutMinutes(minutes) {
		if logger != nil {
			logger.Warn("agent runtime_config: invalid execution.timeout_minutes; using default timeout", "timeout_minutes", minutes)
		}
		return fallback
	}
	return time.Duration(minutes * float64(time.Minute))
}

func isValidExecutionTimeoutMinutes(minutes float64) bool {
	return !math.IsNaN(minutes) && !math.IsInf(minutes, 0) && minutes > 0 && minutes <= 24*60
}
