package main

import (
	_ "embed"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed icon.ico
var trayIconBytes []byte

// startTray 启动系统托盘（在独立 goroutine 中调用，自身阻塞）
//
// 关闭路径：
//   - 用户点托盘「退出」→ 调 a.Quit() → runtime.Quit → Wails 主循环退出 →
//     app.shutdown 完成清理 → main 返回前调 systray.Quit 退出托盘
//   - 用户从其它途径退出（暂无）：保持一致
func startTray(app *App) {
	systray.Run(func() {
		systray.SetIcon(trayIconBytes)
		systray.SetTitle("Shadowsocks")
		systray.SetTooltip("Shadowsocks 客户端")

		mStatus := systray.AddMenuItem("Shadowsocks", "")
		mStatus.Disable()
		systray.AddSeparator()

		mShow := systray.AddMenuItem("显示窗口", "")
		mToggle := systray.AddMenuItem("启动/停止代理", "")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "")

		// 菜单事件回调（直接注册 Click handler）
		mShow.Click(func() { runtime.WindowShow(app.ctx) })
		mToggle.Click(func() {
			if _, err := app.ToggleProxy(); err != nil {
				runtime.EventsEmit(app.ctx, "log", "[托盘] "+err.Error())
			}
			// 通知前端刷新（前端监听此事件）
			runtime.EventsEmit(app.ctx, "tray-action", "toggle")
		})
		mQuit.Click(func() { app.Quit() })
	}, func() {
		// onExit：托盘结束（通常由 systray.Quit 触发），无需额外操作
	})
}

// quitTray 通知托盘退出（在 main 里 wails.Run 返回后调用）
func quitTray() {
	systray.Quit()
}
