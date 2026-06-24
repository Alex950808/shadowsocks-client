package proxy

import (
	"fmt"
	"log"
	"golang.org/x/sys/windows/registry"
)

var origEnabled bool
var origServer string
var origOverride string
var origOverrideOk bool // 是否曾成功读到原始 ProxyOverride（区分“原本为空”与“原本无此值”）

const regPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// SetSystemProxy 设置 Windows 系统代理
func SetSystemProxy(port int) {
	backupSystemProxy()

	proxy := fmt.Sprintf("127.0.0.1:%d", port)

	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	if err != nil {
		log.Printf("[系统代理] 打开注册表失败: %v", err)
		return
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		log.Printf("[系统代理] ProxyEnable 设置失败: %v", err)
		return
	}
	if err := k.SetStringValue("ProxyServer", proxy); err != nil {
		log.Printf("[系统代理] ProxyServer 设置失败: %v", err)
		return
	}	
	k.SetStringValue("ProxyOverride", "127.0.0.1;localhost;<local>;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;192.168.*;*.local")

	log.Printf("[系统代理] 已启用 %s", proxy)
}

// RestoreSystemProxy 恢复原始系统代理
func RestoreSystemProxy() {
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.SET_VALUE)
	if err != nil {
		log.Printf("[系统代理] 打开注册表失败: %v", err)
		return
	}
	defer k.Close()

	e := uint32(0)
	if origEnabled {
		e = 1
	}
	k.SetDWordValue("ProxyEnable", e)
	k.SetStringValue("ProxyServer", origServer)

	// 还原 ProxyOverride：原本有此值则写回，原本没有则删除我们写入的（忽略“值已不存在”等错误）
	if origOverrideOk {
		k.SetStringValue("ProxyOverride", origOverride)
	} else {
		_ = k.DeleteValue("ProxyOverride")
	}

	log.Println("[系统代理] 已恢复")
}

func backupSystemProxy() {
	k, err := registry.OpenKey(registry.CURRENT_USER, regPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	v, _, err := k.GetIntegerValue("ProxyEnable")
	if err == nil && v == 1 {
		origEnabled = true
	}

	s, _, err := k.GetStringValue("ProxyServer")
	if err == nil {
		origServer = s
	}

	// 备份原始 ProxyOverride，以便关闭代理时还原/删除我们写入的值
	if o, _, e := k.GetStringValue("ProxyOverride"); e == nil {
		origOverride = o
		origOverrideOk = true
	}
}
