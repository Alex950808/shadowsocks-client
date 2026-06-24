package main

import (
	"log"
	"os"

	"shadowsocks-client/internal/single"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	// 单实例锁：防止同时运行多个实例
	if !single.TryLock("Shadowsocks") {
		log.Println("已有实例在运行，退出")
		os.Exit(0)
	}

	app := NewApp()

	// 启动系统托盘（独立 goroutine 阻塞跑）
	go startTray(app)

	err := wails.Run(&options.App{
		Title:         "Shadowsocks",
		Width:         640,
		Height:        680,
		DisableResize: false,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		// ✕ / Alt+F4 时隐藏到托盘而不是退出程序
		// 真正退出只能走托盘菜单「退出」
		HideWindowOnClose: true,
		BackgroundColour:  &options.RGBA{R: 15, G: 15, B: 26, A: 255},
		Bind: []interface{}{
			app,
		},
		// 拦截窗口关闭：✕ / Alt+F4 / 任务栏关闭 都走这里
		// 返回 false 阻止关闭，把窗口隐藏到托盘。
		// 真正退出只走托盘菜单「退出」→ App.Quit() → runtime.Quit
	})

	// Wails 主循环结束（Users 通过托盘退出）后，也把托盘停掉
	quitTray()

	if err != nil {
		log.Fatal(err)
	}
}
