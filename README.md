# Shadowsocks Client

Windows 桌面 Shadowsocks 客户端，基于 Wails v2 + Go 构建。

## 功能

- HTTP/SOCKS5 代理
- 系统托盘快速切换
- 国内站点智能直连（GeoLite2-Country + 域名规则）
- Windows 系统代理自动配置

## 构建

```powershell
# 开发模式
wails dev

# 生产构建 + UPX 压缩
.\build.ps1
```

## 项目结构

| 目录 | 说明 |
|---|---|
| `main.go` | Wails 入口 |
| `app.go` | 应用生命周期 + 7 个 JS 绑定方法 |
| `tray.go` | 系统托盘 |
| `internal/config/` | JSON 配置读写 |
| `internal/proxy/` | 代理引擎（HTTP/SOCKS5/SS 加密/分流） |
| `internal/single/` | 单实例锁 |
| `frontend/` | 单文件 SPA（无构建工具） |
