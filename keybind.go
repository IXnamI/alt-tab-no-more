package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Keymap struct {
	KeyBind     string `json:"keybind"`
	BindSetting struct {
		ApplicationType string   `json:"type"`
		ApplicationPath string   `json:"path"`
		WindowName      string   `json:"winName"`
		Args            []string `json:"args"`
	} `json:"settings"`
}

type KeybindExecutable interface {
	Type() KeybindType
	Open() (windows.Handle, error)
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

func (tk *TerminalKeybind) Open() (windows.Handle, error) {
	handle, err := focusWT(tk.windowName, tk.args...)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return handle, nil
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

func (ak *AppKeybind) Open() (windows.Handle, error) {
	cmd := exec.Command(ak.execPath, ak.args...)
	err := cmd.Start()
	if err != nil {
		fmt.Println("Error:", err)
		return windows.InvalidHandle, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(cmd.Process.Pid))
	if err != nil {
		fmt.Println("OpenProcess failed:", err)
		return windows.InvalidHandle, err
	}
	return h, nil
}

type KeybindType int

const (
	Terminal KeybindType = iota
	App
)

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

func extractKeybindMappingFromFile() {
	jsonFile, err := os.Open("keymaps.json")
	if err != nil {
		fmt.Println("No keymap was found")
	}
	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		fmt.Println("Could not load keymap into buffer")
	}
	var keymappings []Keymap
	err = json.Unmarshal(byteValue, &keymappings)
	if err != nil {
		fmt.Println("Could not parse JSON keymap")
		return
	}
	for i := range len(keymappings) {
		bindSetting := keymappings[i].BindSetting
		keybind := keymappings[i].KeyBind
		keybindType := parseKeybindType(bindSetting.ApplicationType)
		fmt.Println("KeybindType:", keybindType)
		fmt.Printf("Keybind: %s, mapped to application: %s\n", keymappings[i].KeyBind, keymappings[i].BindSetting)
		stringKeybind, err := parseKeybind(keybind)
		fmt.Printf("String keybind: %s\n", stringKeybind)
		if err != nil {
			fmt.Println(err)
			return
		}
		switch keybindType {
		case Terminal:
			tk := &TerminalKeybind{
				kType:         parseKeybindType(bindSetting.ApplicationType),
				windowName:    bindSetting.WindowName,
				designatedKey: stringKeybind,
				args:          bindSetting.Args,
			}
			keybindMappings[stringKeybind] = tk
		case App:
			tk := &AppKeybind{
				kType:         parseKeybindType(bindSetting.ApplicationType),
				execPath:      bindSetting.ApplicationPath,
				designatedKey: stringKeybind,
				args:          bindSetting.Args,
			}
			keybindMappings[stringKeybind] = tk
		}
	}
}

func parseKeybind(keybind string) (string, error) {
	keybindWithoutLeader, found := strings.CutPrefix(keybind, "<leader>")
	if !found {
		fmt.Errorf("Could not parse keybind")
		return "", fmt.Errorf("Could not parse keybind")
	}
	return keybindWithoutLeader, nil
}

func parseKeybindType(kType string) KeybindType {
	lowercase := strings.ToLower(kType)
	switch lowercase {
	case "terminal":
		return Terminal
	default:
		return App
	}
}
