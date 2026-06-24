package single

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
)

const (
	ERROR_ALREADY_EXISTS = 183
	SW_RESTORE           = 9
)

// TryLock 获取单实例互斥锁。如果已有一个实例在运行，将已有窗口带到前台并返回 false。
func TryLock(windowTitle string) bool {
	name, _ := syscall.UTF16PtrFromString("ShadowsocksClient_SingleInstance_Mutex")
	_, _, e := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if errno, ok := e.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
		bringToFront(windowTitle)
		return false
	}
	return true // 互斥体由进程持有，进程退出时自动释放
}

// bringToFront 查找标题匹配的窗口并带到前台
func bringToFront(title string) {
	t, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	if hwnd != 0 {
		procShowWindow.Call(hwnd, SW_RESTORE)
		procSetForegroundWindow.Call(hwnd)
	}
}
