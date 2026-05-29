package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerPort int              `yaml:"server_port" json:"server_port"`
	Timezone   string           `yaml:"timezone" json:"timezone"`
	InputCSV   string           `yaml:"input_csv" json:"input_csv"`
	Massive    MassiveConfig    `yaml:"massive" json:"massive"`
	Live       LiveConfig       `yaml:"live" json:"live"`
	Historical HistoricalConfig `yaml:"historical" json:"historical"`
	Scoring    ScoringConfig    `yaml:"scoring" json:"scoring"`
	UI         UIConfig         `yaml:"ui" json:"ui"`
	Logging    LoggingConfig    `yaml:"logging" json:"logging"`
}

type MassiveConfig struct {
	CacheDir            string `yaml:"cache_dir" json:"cache_dir"`
	MaxParallelRequests int    `yaml:"max_parallel_requests" json:"max_parallel_requests"`
}

type LiveConfig struct {
	Enabled        bool   `yaml:"enabled" json:"enabled"`
	RefreshSeconds int    `yaml:"refresh_seconds" json:"refresh_seconds"`
	StartTime      string `yaml:"start_time" json:"start_time"`
	EndTime        string `yaml:"end_time" json:"end_time"`
}

type HistoricalConfig struct {
	StartTime         string `yaml:"start_time" json:"start_time"`
	EndTime           string `yaml:"end_time" json:"end_time"`
	ReplayStepSeconds int    `yaml:"replay_step_seconds" json:"replay_step_seconds"`
}

type ScoringConfig struct {
	LookbackMinutes      int     `yaml:"lookback_minutes" json:"lookback_minutes"`
	RangeLookbackMinutes int     `yaml:"range_lookback_minutes" json:"range_lookback_minutes"`
	ATRPeriod            int     `yaml:"atr_period" json:"atr_period"`
	MinDollarVolume      float64 `yaml:"min_dollar_volume" json:"min_dollar_volume"`
	ExcellentScore       float64 `yaml:"excellent_score" json:"excellent_score"`
	GoodScore            float64 `yaml:"good_score" json:"good_score"`
}

type UIConfig struct {
	ChartOpenerBaseURL string `yaml:"chart_opener_base_url" json:"chart_opener_base_url"`
}

type LoggingConfig struct {
	Level string `yaml:"level" json:"level"`
}

func Default() Config {
	return Config{
		ServerPort: 8089,
		Timezone:   "America/New_York",
		InputCSV:   "top-100.csv",
		Massive: MassiveConfig{
			CacheDir:            "cache",
			MaxParallelRequests: 6,
		},
		Live: LiveConfig{
			Enabled:        true,
			RefreshSeconds: 60,
			StartTime:      "04:00",
			EndTime:        "20:00",
		},
		Historical: HistoricalConfig{
			StartTime:         "04:00",
			EndTime:           "20:00",
			ReplayStepSeconds: 60,
		},
		Scoring: ScoringConfig{
			LookbackMinutes:      30,
			RangeLookbackMinutes: 60,
			ATRPeriod:            14,
			MinDollarVolume:      10_000_000,
			ExcellentScore:       75,
			GoodScore:            60,
		},
		UI: UIConfig{
			ChartOpenerBaseURL: "http://localhost:8081",
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	cfg.Normalize()
	return cfg, cfg.Validate()
}

func (c *Config) Normalize() {
	def := Default()
	if c.ServerPort == 0 {
		c.ServerPort = def.ServerPort
	}
	if strings.TrimSpace(c.Timezone) == "" {
		c.Timezone = def.Timezone
	}
	if strings.TrimSpace(c.InputCSV) == "" {
		c.InputCSV = def.InputCSV
	}
	if strings.TrimSpace(c.Massive.CacheDir) == "" {
		c.Massive.CacheDir = def.Massive.CacheDir
	}
	if c.Massive.MaxParallelRequests <= 0 {
		c.Massive.MaxParallelRequests = def.Massive.MaxParallelRequests
	}
	if c.Live.RefreshSeconds <= 0 {
		c.Live.RefreshSeconds = def.Live.RefreshSeconds
	}
	if strings.TrimSpace(c.Live.StartTime) == "" {
		c.Live.StartTime = def.Live.StartTime
	}
	if strings.TrimSpace(c.Live.EndTime) == "" {
		c.Live.EndTime = def.Live.EndTime
	}
	if strings.TrimSpace(c.Historical.StartTime) == "" {
		c.Historical.StartTime = def.Historical.StartTime
	}
	if strings.TrimSpace(c.Historical.EndTime) == "" {
		c.Historical.EndTime = def.Historical.EndTime
	}
	if c.Historical.ReplayStepSeconds <= 0 {
		c.Historical.ReplayStepSeconds = def.Historical.ReplayStepSeconds
	}
	if c.Scoring.LookbackMinutes <= 0 {
		c.Scoring.LookbackMinutes = def.Scoring.LookbackMinutes
	}
	if c.Scoring.RangeLookbackMinutes <= 0 {
		c.Scoring.RangeLookbackMinutes = def.Scoring.RangeLookbackMinutes
	}
	if c.Scoring.ATRPeriod <= 0 {
		c.Scoring.ATRPeriod = def.Scoring.ATRPeriod
	}
	if c.Scoring.MinDollarVolume <= 0 {
		c.Scoring.MinDollarVolume = def.Scoring.MinDollarVolume
	}
	if c.Scoring.ExcellentScore <= 0 {
		c.Scoring.ExcellentScore = def.Scoring.ExcellentScore
	}
	if c.Scoring.GoodScore <= 0 {
		c.Scoring.GoodScore = def.Scoring.GoodScore
	}
	if strings.TrimSpace(c.UI.ChartOpenerBaseURL) == "" {
		c.UI.ChartOpenerBaseURL = def.UI.ChartOpenerBaseURL
	}
	if strings.TrimSpace(c.Logging.Level) == "" {
		c.Logging.Level = def.Logging.Level
	}
}

func (c Config) Validate() error {
	if c.ServerPort <= 0 {
		return fmt.Errorf("server_port must be > 0")
	}
	if _, err := time.Parse("15:04", c.Live.StartTime); err != nil {
		return fmt.Errorf("live.start_time: %w", err)
	}
	if _, err := time.Parse("15:04", c.Live.EndTime); err != nil {
		return fmt.Errorf("live.end_time: %w", err)
	}
	if _, err := time.Parse("15:04", c.Historical.StartTime); err != nil {
		return fmt.Errorf("historical.start_time: %w", err)
	}
	if _, err := time.Parse("15:04", c.Historical.EndTime); err != nil {
		return fmt.Errorf("historical.end_time: %w", err)
	}
	return nil
}

func APIKeyFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("MASSIVE_API_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("POLYGON_API_KEY"))
}

func MustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err == nil {
		return loc
	}
	fallback, _ := time.LoadLocation("America/New_York")
	return fallback
}
