package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ServerConfig 单个 Shadowsocks 服务器配置
type ServerConfig struct {
	Name     string `json:"name"`     // 节点名称
	Server   string `json:"server"`   // 服务器地址
	Port     int    `json:"port"`     // 服务器端口
	Password string `json:"password"` // 密码
	Method   string `json:"method"`   // 加密方式
}

// AppConfig 应用程序配置
type AppConfig struct {
	HttpPort   int            `json:"http_port"`   // HTTP 代理端口
	SocksPort  int            `json:"socks_port"`  // SOCKS5 代理端口
	Servers    []ServerConfig `json:"servers"`     // 服务器列表
	ActiveIdx  int            `json:"active_idx"`  // 当前活跃服务器索引，-1 表示无
	AutoStart  bool           `json:"auto_start"`  // 是否自动启动
}

// DefaultConfig 返回默认配置
func DefaultConfig() *AppConfig {
	return &AppConfig{
		HttpPort:  8899,
		SocksPort: 1080,
		Servers:   []ServerConfig{},
		ActiveIdx: -1,
		AutoStart: false,
	}
}

// ConfigFilePath 获取配置文件路径
func ConfigFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".shadowsocks-client")
	os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "config.json")
}

// Load 从文件加载配置
func Load() (*AppConfig, error) {
	path := ConfigFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save 保存配置到文件
func (c *AppConfig) Save() error {
	path := ConfigFilePath()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
