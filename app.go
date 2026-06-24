package main

import (
	"context"
	"log"

	"shadowsocks-client/internal/config"
	"shadowsocks-client/internal/proxy"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App Wails 绑定结构体。所有 UI 操作通过方法绑定暴露给前端
type App struct {
	ctx    context.Context
	cfg    *config.AppConfig
	client *proxy.Client
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.cfg, _ = config.Load()
	if a.cfg == nil {
		a.cfg = config.DefaultConfig()
	}

	a.client = proxy.New()

	// 订阅 proxy 内部日志并通过 Wails 事件推送给前端
	go func() {
		ch := proxy.SubscribeLogs()
		for msg := range ch {
			runtime.EventsEmit(ctx, "log", msg)
		}
	}()

	log.Println("Shadowsocks 桌面版已启动")
}

func (a *App) shutdown(ctx context.Context) {
	if a.client != nil {
		a.client.Stop()
	}
	if a.cfg != nil {
		a.cfg.Save()
	}
	log.Println("已退出")
}

// Quit 从 JS 调用退出
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}

// ===== 内部辅助 =====

// validIdx 检查 idx 是否指向有效服务器；越界返回 false
func (a *App) validIdx(idx int) bool {
	return idx >= 0 && idx < len(a.cfg.Servers)
}

// startActive 启动当前 ActiveIdx 指向的节点（调用方已确认有效）
func (a *App) startActive() error {
	s := a.cfg.Servers[a.cfg.ActiveIdx]
	return a.client.Start(s.Server, s.Port, s.Password, s.Method, a.cfg.HttpPort, a.cfg.SocksPort)
}

// ===== 绑定方法（前端调用）=====

// StatusResponse 状态查询返回
type StatusResponse struct {
	Running   bool                   `json:"running"`
	ActiveIdx int                    `json:"active_idx"`
	HttpPort  int                    `json:"http_port"`
	SocksPort int                    `json:"socks_port"`
	Servers   []config.ServerConfig  `json:"servers"`
}

// GetStatus 返回当前运行状态与服务器列表
func (a *App) GetStatus() StatusResponse {
	return StatusResponse{
		Running:   a.client.Running(),
		ActiveIdx: a.cfg.ActiveIdx,
		HttpPort:  a.cfg.HttpPort,
		SocksPort: a.cfg.SocksPort,
		Servers:   a.cfg.Servers,
	}
}

// SaveConfig 更新代理端口
func (a *App) SaveConfig(httpPort, socksPort int) error {
	if httpPort > 0 && httpPort < 65536 {
		a.cfg.HttpPort = httpPort
	}
	if socksPort > 0 && socksPort < 65536 {
		a.cfg.SocksPort = socksPort
	}
	a.cfg.Save()
	return nil
}

// ServerInput 服务器输入参数（前端调用 SaveServer 时传入）
// 用 struct 而非多位置参数，避免顺序写错
type ServerInput struct {
	Idx      int    `json:"idx"`      // >=0 编辑；<0 新增
	Name     string `json:"name"`     // 节点名称
	Server   string `json:"server"`   // 服务器地址
	Port     int    `json:"port"`     // 端口
	Password string `json:"password"` // 密码
	Method   string `json:"method"`   // 加密方式
}

// SaveServer 新增或编辑服务器（按 in.Idx，<0 表示新增）
func (a *App) SaveServer(in ServerInput) error {
	s := config.ServerConfig{
		Name: in.Name, Server: in.Server, Port: in.Port,
		Password: in.Password, Method: in.Method,
	}
	if a.validIdx(in.Idx) {
		a.cfg.Servers[in.Idx] = s
	} else {
		a.cfg.Servers = append(a.cfg.Servers, s)
	}
	a.cfg.Save()
	return nil
}

// DeleteServer 删除服务器
func (a *App) DeleteServer(idx int) error {
	if !a.validIdx(idx) {
		return nil
	}
	if idx == a.cfg.ActiveIdx {
		a.client.Stop()
		a.cfg.ActiveIdx = -1
	} else if idx < a.cfg.ActiveIdx {
		a.cfg.ActiveIdx--
	}
	a.cfg.Servers = append(a.cfg.Servers[:idx], a.cfg.Servers[idx+1:]...)
	a.cfg.Save()
	return nil
}

// SwitchServer 切换并启动指定节点
func (a *App) SwitchServer(idx int) error {
	if !a.validIdx(idx) {
		return errInvalidIdx
	}
	a.client.Stop()
	a.cfg.ActiveIdx = idx
	a.cfg.Save()
	return a.startActive()
}

// ToggleResponse 切换结果
type ToggleResponse struct {
	Action string `json:"action"` // "started" 或 "stopped"
}

// ToggleProxy 启停切换。返回当前动作
func (a *App) ToggleProxy() (ToggleResponse, error) {
	if a.client.Running() {
		a.client.Stop()
		a.cfg.ActiveIdx = -1
		a.cfg.Save()
		return ToggleResponse{Action: "stopped"}, nil
	}
	if len(a.cfg.Servers) == 0 {
		return ToggleResponse{}, errNoServers
	}
	if a.cfg.ActiveIdx < 0 {
		a.cfg.ActiveIdx = 0
		a.cfg.Save()
	}
	if err := a.startActive(); err != nil {
		return ToggleResponse{}, err
	}
	return ToggleResponse{Action: "started"}, nil
}
