package config

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var (
	_config   *config
	_configMu sync.RWMutex
)

const defaultConfigPath = "conf.d/config.yaml"

type config struct {
	cfg         *Config
	cfgChangeCh chan ConfigUpdate
	watchOnce   sync.Once
	mu          sync.RWMutex
	stopCh      chan struct{}
	viper       *viper.Viper
}

// ConfigUpdate 配置更新通知。
type ConfigUpdate struct {
	Config *Config
	Error  error
	Time   time.Time
}

// Config 应用配置。
type Config struct {
	App      AppConfig      `json:"app" yaml:"app" mapstructure:"app"`
	Server   ServerConfig   `json:"server" yaml:"server" mapstructure:"server"`
	Database DatabaseConfig `json:"database" yaml:"database" mapstructure:"database"`
	Security SecurityConfig `json:"security" yaml:"security" mapstructure:"security"`
	Logger   LoggerConfig   `json:"logger" yaml:"logger" mapstructure:"logger"`
	Qwen     QwenConfig     `json:"qwen" yaml:"qwen" mapstructure:"qwen"`
	Chunk    ChunkConfig    `json:"chunk" yaml:"chunk" mapstructure:"chunk"`
	Vector   VectorConfig   `json:"vector" yaml:"vector" mapstructure:"vector"`
	// 调用观测相关开关
	Observability ObservabilityConfig `json:"observability" yaml:"observability" mapstructure:"observability"`
}

// ObservabilityConfig 调用观测配置。
type ObservabilityConfig struct {
	// LogEmbedding 是否记录向量（Embedding）调用。
	//
	// 默认 false：向量调用频率远高于对话（一次检索一次），单价却低一到两个数量级，
	// 全量记录会把观测页主列表淹没，反而看不清真正花钱的 LLM 调用。
	// 需要核对向量成本时把它打开即可。
	LogEmbedding bool `json:"logEmbedding" yaml:"logEmbedding" mapstructure:"logEmbedding"`
}

type AppConfig struct {
	Name string `json:"name" yaml:"name" mapstructure:"name"`
	Env  string `json:"env" yaml:"env" mapstructure:"env"`
}

type ServerConfig struct {
	Host string `json:"host" yaml:"host" mapstructure:"host"`
	Port int    `json:"port" yaml:"port" mapstructure:"port"`
}

type DatabaseConfig struct {
	Driver          string        `json:"driver" yaml:"driver" mapstructure:"driver"`
	DSN             string        `json:"dsn" yaml:"dsn" mapstructure:"dsn"`
	MaxIdleConns    int           `json:"maxIdleConns" yaml:"maxIdleConns" mapstructure:"maxIdleConns"`
	MaxOpenConns    int           `json:"maxOpenConns" yaml:"maxOpenConns" mapstructure:"maxOpenConns"`
	ConnMaxLifetime time.Duration `json:"connMaxLifetime" yaml:"connMaxLifetime" mapstructure:"connMaxLifetime"`
	// LogLevel GORM 日志级别：silent / error / warn / info（默认 warn，只打印错误和慢查询）
	LogLevel string `json:"logLevel" yaml:"logLevel" mapstructure:"logLevel"`
}

type SecurityConfig struct {
	JWTSecret        string `json:"jwtSecret" yaml:"jwtSecret" mapstructure:"jwtSecret"`
	TokenExpireHours int    `json:"tokenExpireHours" yaml:"tokenExpireHours" mapstructure:"tokenExpireHours"`
}

type LoggerConfig struct {
	Level    string `json:"level" yaml:"level" mapstructure:"level"`
	FilePath string `json:"filePath" yaml:"filePath" mapstructure:"filePath"`
	MaxSize  int    `json:"maxSize" yaml:"maxSize" mapstructure:"maxSize"`
}

type QwenConfig struct {
	APIKey     string `json:"apiKey" yaml:"apiKey" mapstructure:"apiKey"`
	ChatModel  string `json:"chatModel" yaml:"chatModel" mapstructure:"chatModel"`
	EmbedModel string `json:"embedModel" yaml:"embedModel" mapstructure:"embedModel"`
	BaseURL    string `json:"baseUrl" yaml:"baseUrl" mapstructure:"baseUrl"`
}

type ChunkConfig struct {
	Size    int `json:"size" yaml:"size" mapstructure:"size"`
	Overlap int `json:"overlap" yaml:"overlap" mapstructure:"overlap"`
}

type VectorConfig struct {
	TopK      int     `json:"topK" yaml:"topK" mapstructure:"topK"`
	Threshold float64 `json:"threshold" yaml:"threshold" mapstructure:"threshold"`
}

func New() *Config {
	return &Config{
		App: AppConfig{Name: "aiagent", Env: "dev"},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Database: DatabaseConfig{
			Driver:          "postgres",
			DSN:             "host=127.0.0.1 user=postgres password=postgres dbname=aiagent port=5432 sslmode=disable",
			MaxIdleConns:    10,
			MaxOpenConns:    100,
			ConnMaxLifetime: 2 * time.Hour,
		},
		Security: SecurityConfig{
			JWTSecret:        "aiagent-secret-key",
			TokenExpireHours: 24,
		},
		Logger: LoggerConfig{
			Level:    "info",
			FilePath: "./logs",
			MaxSize:  100,
		},
		Qwen: QwenConfig{
			ChatModel:  "qwen-plus",
			EmbedModel: "text-embedding-v3",
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		},
		Chunk: ChunkConfig{
			Size:    512,
			Overlap: 64,
		},
		Vector: VectorConfig{
			TopK:      5,
			Threshold: 0.7,
		},
	}
}

func (c *Config) DeepCopy() *Config {
	if c == nil {
		return New()
	}
	cp := *c
	return &cp
}

func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	if c.Security.JWTSecret == "" {
		return fmt.Errorf("jwt secret cannot be empty")
	}
	return nil
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func defaultConfig(path string) *config {
	if path == "" {
		path = defaultConfigPath
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("AIAGENT")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	return &config{
		cfg:         New(),
		cfgChangeCh: make(chan ConfigUpdate, 5),
		stopCh:      make(chan struct{}),
		viper:       v,
	}
}

func (c *config) loadFromDisk() (*Config, error) {
	if c.viper == nil {
		return nil, fmt.Errorf("viper not initialized")
	}
	if err := c.viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return c.cfg.DeepCopy(), nil
		}
		return nil, err
	}
	temp := New()
	if err := c.viper.Unmarshal(temp); err != nil {
		return nil, err
	}
	if err := temp.Validate(); err != nil {
		return nil, err
	}
	c.cfg = temp
	return c.cfg.DeepCopy(), nil
}

func (c *config) GetConfig() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg.DeepCopy()
}

func TryLoadFromDisk(path string) (*Config, error) {
	_configMu.Lock()
	defer _configMu.Unlock()
	if _config != nil {
		return _config.GetConfig(), nil
	}
	_config = defaultConfig(path)
	cfg, err := _config.loadFromDisk()
	if err != nil {
		_config = nil
		return nil, err
	}
	return cfg, nil
}

func GetCurrentConfig() *Config {
	_configMu.RLock()
	defer _configMu.RUnlock()
	if _config == nil {
		return New()
	}
	return _config.GetConfig()
}

func WatchConfigChange() <-chan ConfigUpdate {
	_configMu.RLock()
	defer _configMu.RUnlock()
	if _config == nil {
		ch := make(chan ConfigUpdate)
		close(ch)
		return ch
	}
	_config.watchOnce.Do(func() {
		if _config.viper == nil {
			return
		}
		_config.viper.WatchConfig()
		_config.viper.OnConfigChange(func(e fsnotify.Event) {
			select {
			case <-_config.stopCh:
				return
			default:
			}
			temp := New()
			if err := _config.viper.Unmarshal(temp); err != nil {
				_config.send(ConfigUpdate{Error: err, Time: time.Now()})
				return
			}
			if err := temp.Validate(); err != nil {
				_config.send(ConfigUpdate{Error: err, Time: time.Now()})
				return
			}
			_config.mu.Lock()
			_config.cfg = temp
			_config.mu.Unlock()
			_config.send(ConfigUpdate{Config: temp.DeepCopy(), Time: time.Now()})
		})
	})
	return _config.cfgChangeCh
}

func StopWatching() {
	_configMu.Lock()
	defer _configMu.Unlock()
	if _config != nil {
		select {
		case <-_config.stopCh:
		default:
			close(_config.stopCh)
		}
	}
}

func (c *config) send(u ConfigUpdate) {
	select {
	case c.cfgChangeCh <- u:
	default:
	}
}

func EnsureConfigFile() error {
	if _, err := os.Stat(defaultConfigPath); err == nil {
		return nil
	}
	os.MkdirAll("conf.d", 0755)
	return os.WriteFile(defaultConfigPath, []byte(defaultYAML), 0644)
}

const defaultYAML = `app:
  name: aiagent
  env: dev

server:
  host: 0.0.0.0
  port: 8080

database:
  driver: postgres
  dsn: "host=127.0.0.1 user=postgres password=postgres dbname=aiagent port=5432 sslmode=disable TimeZone=Asia/Shanghai"
  maxIdleConns: 10
  maxOpenConns: 100
  connMaxLifetime: 2h

security:
  jwtSecret: aiagent-secret-key
  tokenExpireHours: 24

logger:
  level: info
  filePath: ./logs
  maxSize: 100

qwen:
  apiKey: ""
  chatModel: qwen-plus
  embedModel: text-embedding-v3
  baseUrl: https://dashscope.aliyuncs.com/compatible-mode/v1

chunk:
  size: 512
  overlap: 64

vector:
  topK: 5
  threshold: 0.7
`