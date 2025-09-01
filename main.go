package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

type Keymap struct {
	KeyBind           string `json:"keybind"`
	MappedApplication string `json:"mapped"`
	BindSetting       struct {
		ApplicationType string   `json:"type"`
		WindowName      string   `json:"winName"`
		Args            []string `json:"args"`
	} `json:"settings"`
}

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
	appHandles                   = make(map[uint32]windows.Handle)
	keybindMappings              = make(map[string]KeybindExecutable)
	isLeaderPressed              = false
	leaderKeyVK                  = uint32(0x41)
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
)

type kbdLLHookStruct struct {
	VKCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var KeyToVK = map[string]uint32{
	// Letters
	"A": 0x41, "B": 0x42, "C": 0x43, "D": 0x44,
	"E": 0x45, "F": 0x46, "G": 0x47, "H": 0x48,
	"I": 0x49, "J": 0x4A, "K": 0x4B, "L": 0x4C,
	"M": 0x4D, "N": 0x4E, "O": 0x4F, "P": 0x50,
	"Q": 0x51, "R": 0x52, "S": 0x53, "T": 0x54,
	"U": 0x55, "V": 0x56, "W": 0x57, "X": 0x58,
	"Y": 0x59, "Z": 0x5A,

	// Numbers
	"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33,
	"4": 0x34, "5": 0x35, "6": 0x36, "7": 0x37,
	"8": 0x38, "9": 0x39,

	// Control keys
	"Enter":     uint32(windows.VK_RETURN),
	"Escape":    uint32(windows.VK_ESCAPE),
	"Space":     uint32(windows.VK_SPACE),
	"Tab":       uint32(windows.VK_TAB),
	"Backspace": uint32(windows.VK_BACK),

	// Arrows
	"Left":  uint32(windows.VK_LEFT),
	"Right": uint32(windows.VK_RIGHT),
	"Up":    uint32(windows.VK_UP),
	"Down":  uint32(windows.VK_DOWN),

	// Modifiers
	"Shift": uint32(windows.VK_SHIFT),
	"Ctrl":  uint32(windows.VK_CONTROL),
	"Alt":   uint32(windows.VK_MENU),

	// Function keys
	"F1":  uint32(windows.VK_F1),
	"F2":  uint32(windows.VK_F2),
	"F3":  uint32(windows.VK_F3),
	"F4":  uint32(windows.VK_F4),
	"F5":  uint32(windows.VK_F5),
	"F6":  uint32(windows.VK_F6),
	"F7":  uint32(windows.VK_F7),
	"F8":  uint32(windows.VK_F8),
	"F9":  uint32(windows.VK_F9),
	"F10": uint32(windows.VK_F10),
	"F11": uint32(windows.VK_F11),
	"F12": uint32(windows.VK_F12),
}

type KeybindExecutable interface {
	Type() KeybindType
	Open() error
}

type TerminalKeybind struct {
	kType         KeybindType
	windowName    string
	designatedKey string
	args          []string
}

func (tk *TerminalKeybind) Type() KeybindType {
	return tk.kType
}

func (tk *TerminalKeybind) Open() error {
	handle, err := focusWT(tk.windowName)
	vkCodeKey := KeyToVK[tk.designatedKey]
	appHandles[vkCodeKey] = handle
	if err != nil {
		return err
	}
	return nil
}

type AppKeybind struct {
	kType         KeybindType
	execPath      string
	designatedKey string
	args          []string
}

func (ak *AppKeybind) Type() KeybindType {
	return ak.kType
}

func (ak *AppKeybind) Open() error {
	cmd := exec.Command(ak.execPath)
	return cmd.Run()
}

type KeybindType int

const (
	Terminal KeybindType = iota
	App
)

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
			if keyType, ok := keybindMappings[convertVkToStringInput(vk)]; ok {
				switch keyType.Type() {
				case Terminal:
					if _, err := focusWT("Tab_1"); err != nil {
						fmt.Println("Error:", err)
					}
				case App:
				}
			}
			switch vk {
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
			}
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

var keybindMap map[string]syscall.Handle

func main() {
	jsonFile, err := os.Open("keymaps.json")
	if err != nil {
		fmt.Println("No keymap was found")
	}
	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		fmt.Println("Could not load keymap into buffer")
	}
	var keymappings []Keymap
	json.Unmarshal(byteValue, &keymappings)
	for i := range len(keymappings) {
		fmt.Printf("Keybind: %s, mapped to application: %s\n", keymappings[i].KeyBind, keymappings[i].MappedApplication)
	}
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
	appHandle = windows.Handle(hwnd)
	fmt.Printf("Got handle\n")
	return appHandle, nil
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

	fmt.Printf("FgID: %d, curID: %d\n", &fgTid, &curTid)

	procAttachThreadInput.Call(curTid, fgTid, 1)
	procShowWindow.Call(fgHwnd, syscall.SW_FORCEMINIMIZE)
	procShowWindow.Call(uintptr(hwndTarget), syscall.SW_FORCEMINIMIZE)
	procSetForeground.Call(uintptr(hwndTarget))
	procBringWindowToTop.Call(uintptr(hwndTarget))
	procShowWindow.Call(uintptr(hwndTarget), SW_RESTORE)
	procAttachThreadInput.Call(curTid, fgTid, 0)
}

func convertVkToStringInput(vk uint32) string {
	scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0)

	lParam := (scan << 16)

	buf := make([]uint16, 64)
	procGetKeyNameTextW.Call(
		uintptr(lParam),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	return windows.UTF16ToString(buf)
}
