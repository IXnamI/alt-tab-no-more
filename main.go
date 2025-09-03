package main

import (
	"fmt"
	"golang.org/x/sys/windows"
	"syscall"
	"unsafe"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	user32                       = syscall.NewLazyDLL("user32.dll")
	setWindowsHookEx             = user32.NewProc("SetWindowsHookExW")
	unhookWindowsHookEx          = user32.NewProc("UnhookWindowsHookEx")
	callNextHookEx               = user32.NewProc("CallNextHookEx")
	getMessage                   = user32.NewProc("GetMessageW")
	getAsyncKeyState             = user32.NewProc("GetAsyncKeyState")
	getModuleHandle              = kernel32.NewProc("GetModuleHandleW")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procSetForeground            = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
	procMapVirtualKeyW           = user32.NewProc("MapVirtualKeyW")
	procGetKeyNameTextW          = user32.NewProc("GetKeyNameTextW")
	hookHandle                   windows.Handle
	appHandle                    windows.Handle
	appHandles                   = make(map[string]windows.Handle)
	keybindMappings              = make(map[string]KeybindExecutable)
	isLeaderPressed              = false
	keybindMap                   map[string]syscall.Handle
)

func main() {
	extractKeybindMappingFromFile()
	mod, _ := moduleHandle()
	cb := syscall.NewCallback(hookProc)
	h, _, err := setWindowsHookEx.Call(
		uintptr(WH_KEYBOARD_LL),
		cb,
		uintptr(mod),
		0,
	)
	if h == 0 {
		panic(fmt.Errorf("SetWindowsHookEx failed: %v", err))
	}
	hookHandle = windows.Handle(h)
	defer unhookWindowsHookEx.Call(uintptr(hookHandle))

	fmt.Println("Leader launcher running: Ctrl+Shift+A, then 1 or 2. (Close the console to exit)")

	var msg struct {
		hwnd   uintptr
		msg    uint32
		wparam uintptr
		lparam uintptr
		time   uint32
		pt     struct{ x, y int32 }
	}
	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) == -1 {
			break
		}
	}

}
