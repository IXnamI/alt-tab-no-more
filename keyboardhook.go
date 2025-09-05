package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	VK_MENU        = 0x12
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	LLKHF_UP       = 0x80
	VK_CONTROL     = 0x11
	VK_SHIFT       = 0x10
	Key_1          = 0x31
	Key_2          = 0x32
	Key_3          = 0x33
	Key_4          = 0x34
	Key_5          = 0x35
	Key_6          = 0x36
	classWT        = "CASCADIA_HOSTING_WINDOW_CLASS"
	leaderKeyVK    = uint32(0x41)
)

type kbdLLHookStruct struct {
	VKCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

func hookProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && (wParam == WM_KEYDOWN || lParam == WM_KEYUP) {
		kbd := (*kbdLLHookStruct)(unsafe.Pointer(lParam))
		vk := kbd.VKCode

		if kbd.Flags&LLKHF_UP == 1 {
			return 1
		}

		if !isLeaderPressed {
			if isKeyBeingHeld(int16(VK_CONTROL)) && isKeyBeingHeld(int16(VK_SHIFT)) && vk == leaderKeyVK {
				isLeaderPressed = true
				return 1
			}
		}

		if isLeaderPressed {
			isLeaderPressed = false
			keyPressed := convertVkToStringInput(vk)
			if keybind, ok := keybindMappings[keyPressed]; ok {
				if appHwnd, ok := appHandles[keyPressed]; ok {
					forceForeground(appHwnd)
				} else {
					hwnd, err := keybind.Open()
					if err != nil {
						fmt.Println("Error: ", err)
						return 1
					}
					appHandles[keyPressed] = hwnd
				}
			}
			/* switch vk {
			case uint32(Key_1):
				if _, err := focusWT("Tab_1"); err != nil {
					fmt.Println("Error:", err)
				}
			case uint32(Key_2):
				if _, err := focusWT("Tab_2"); err != nil {
					fmt.Println("Error:", err)
				}
			case uint32(Key_3):
				if _, err := focusWT("Tab_3"); err != nil {
					fmt.Println("Error:", err)
				}
			case uint32(Key_4):
				if _, err := focusWT("Tab_4"); err != nil {
					fmt.Println("Error:", err)
				}
			case uint32(Key_5):
				if _, err := focusWT("Tab_5"); err != nil {
					fmt.Println("Error:", err)
				}
			case uint32(Key_6):
				if _, err := focusWT("Tab_6"); err != nil {
					fmt.Println("Error:", err)
				}
			default:
				return 1
			} */
			return 1
		}
	}
	return callNext(nCode, wParam, lParam)
}

func asyncKey(vkCode int16) int16 {
	ret, _, _ := getAsyncKeyState.Call(uintptr(vkCode))
	return int16(ret)
}

func isKeyBeingHeld(vkCode int16) bool {
	mask := uint16(1 << 15)
	vkCodeTemp := uint16(asyncKey(vkCode)) & mask
	return vkCodeTemp != 0
}

func callNext(nCode int32, wParam, lParam uintptr) uintptr {
	ret, _, _ := callNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func moduleHandle() (uintptr, error) {
	r, _, err := getModuleHandle.Call(0)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func utf16PtrFromString(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func findWindow(class, title string) uintptr {
	r, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(utf16PtrFromString(class))),
		uintptr(unsafe.Pointer(utf16PtrFromString(title))),
	)
	return r
}

func wt(args ...string) error {
	cmd := exec.Command("wt.exe", args...)
	return cmd.Run()
}

func focusWT(winName string, args ...string) (windows.Handle, error) {
	focusArgs := []string{"-w", winName, "focus-tab", "-t", "0"}
	newTabArgs := []string{"-w", winName, "new-tab", "--title", winName, "--suppressApplicationTitle"}

	if appHandle != 0 && windows.IsWindow(windows.HWND(appHandle)) {
		_ = wt(append(focusArgs, args...)...)
		fmt.Println("Bringing to ForeGround")
		forceForeground(appHandle)
		return appHandle, nil
	}

	if err := wt(append(newTabArgs, args...)...); err != nil {
		fmt.Println("Creating new tab")
		return windows.InvalidHandle, fmt.Errorf("wt focus-tab: %w", err)
	}
	hwnd := findWindow(classWT, winName)
	for i := 0; i < 10 && hwnd == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		hwnd = findWindow(classWT, winName)
	}
	if hwnd == 0 {
		return windows.InvalidHandle, fmt.Errorf("WT window not found after create")
	}
	fmt.Printf("Got handle\n")
	return windows.Handle(hwnd), nil
}

func forceForeground(hwndTarget windows.Handle) {

	const SW_RESTORE = 9

	fgHwnd, _, _ := procGetForegroundWindow.Call()
	if fgHwnd == 0 {
		fmt.Println("Cant find fg process")
		return
	}

	var fgTid, curTid uintptr
	procGetWindowThreadProcessId.Call(fgHwnd, 0, uintptr(unsafe.Pointer(&fgTid)))
	curTid, _, _ = procGetCurrentThreadId.Call()

	procAttachThreadInput.Call(curTid, fgTid, 1)
	procShowWindow.Call(fgHwnd, syscall.SW_FORCEMINIMIZE)
	procShowWindow.Call(uintptr(hwndTarget), syscall.SW_FORCEMINIMIZE)
	procSetForeground.Call(uintptr(hwndTarget))
	procBringWindowToTop.Call(uintptr(hwndTarget))
	procShowWindow.Call(uintptr(hwndTarget), SW_RESTORE)
	procAttachThreadInput.Call(curTid, fgTid, 0)
}
