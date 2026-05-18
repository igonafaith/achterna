package config

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server         ServerConfig         `yaml:"server"`
	Admin          AdminConfig          `yaml:"admin"`
	Origin         OriginConfig         `yaml:"origin"`
	Cache          CacheConfig          `yaml:"cache"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
	RateLimit      RateLimitConfig      `yaml:"rate_limit"`
	Logging        LoggingConfig        `yaml:"logging"`
}

type ServerConfig struct {
	Addr            string        `yaml:"addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type AdminConfig struct {
	Addr string `yaml:"addr"`
}

type OriginConfig struct {
	URL             string        `yaml:"url"`
	HostHeader      string        `yaml:"host_header"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	IdleConnTimeout time.Duration `yaml:"idle_conn_timeout"`
}

type CacheConfig struct {
	Enabled              bool     `yaml:"enabled"`
	MaxSizeMB            int      `yaml:"max_size_mb"`
	DefaultTTL           time.Duration `yaml:"default_ttl"`
	CacheableExtensions  []string `yaml:"cacheable_extensions"`
	BypassMethods        []string `yaml:"bypass_methods"`
}

type CircuitBreakerConfig struct {
	FailureThreshold  int           `yaml:"failure_threshold"`
	SuccessThreshold  int           `yaml:"success_threshold"`
	Timeout           time.Duration `yaml:"timeout"`
	CountedStatusCodes []int        `yaml:"counted_status_codes"`
}

type RateLimitConfig struct {
	RequestsPerSecond int `yaml:"requests_per_second"`
	Burst             int `yaml:"burst"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.setDefaults()
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Origin.URL == "" {
		return fmt.Errorf("origin.url is required")
	}
	u, err := url.Parse(c.Origin.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("origin.url must include scheme (e.g. https://example.com)")
	}
	if c.Server.Addr == "" {
		return fmt.Errorf("server.addr is required")
	}
	if c.Admin.Addr == "" {
		return fmt.Errorf("admin.addr is required")
	}
	return nil
}

func (c *Config) setDefaults() {
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 30 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 60 * time.Second
	}
	if c.Server.IdleTimeout == 0 {
		c.Server.IdleTimeout = 120 * time.Second
	}
	if c.Server.ShutdownTimeout == 0 {
		c.Server.ShutdownTimeout = 15 * time.Second
	}
	if c.Origin.Timeout == 0 {
		c.Origin.Timeout = 15 * time.Second
	}
	if c.Origin.MaxIdleConns == 0 {
		c.Origin.MaxIdleConns = 100
	}
	if c.Origin.IdleConnTimeout == 0 {
		c.Origin.IdleConnTimeout = 90 * time.Second
	}
	if c.Cache.MaxSizeMB == 0 {
		c.Cache.MaxSizeMB = 256
	}
	if c.Cache.DefaultTTL == 0 {
		c.Cache.DefaultTTL = 5 * time.Minute
	}
	if c.CircuitBreaker.FailureThreshold == 0 {
		c.CircuitBreaker.FailureThreshold = 5
	}
	if c.CircuitBreaker.SuccessThreshold == 0 {
		c.CircuitBreaker.SuccessThreshold = 3
	}
	if c.CircuitBreaker.Timeout == 0 {
		c.CircuitBreaker.Timeout = 30 * time.Second
	}
	if c.RateLimit.RequestsPerSecond == 0 {
		c.RateLimit.RequestsPerSecond = 200
	}
	if c.RateLimit.Burst == 0 {
		c.RateLimit.Burst = 50
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
	if c.Origin.HostHeader == "" {
		u, err := url.Parse(c.Origin.URL)
		if err == nil {
			c.Origin.HostHeader = u.Host
		} else {
			c.Origin.HostHeader = c.Origin.URL
		}
	}
}
