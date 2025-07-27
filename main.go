package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
	"golang.org/x/term"
)

type Config struct {
	Window_Width    int     `json:"window_width"`
	Window_Height   int     `json:"window_height"`
	TerminalRatio   float64 `json:"terminal_ratio"`
	KeyboardRatio   float64 `json:"keyboard_ratio"`
	Font            string  `json:"font"`
	FontSize        int     `json:"font_size"`
	StartCmd        string  `json:"start_cmd"`
	terminal_height int
	keyboard_height int
	char_width      int
	char_height     int
}

const (
	//	WINDOW_WIDTH   = 1280
	// WINDOW_HEIGHT  = 720
	// TERMINAL_RATIO = 0.65
	// KEYBOARD_RATIO = 0.35
	// FONT_SIZE      = 20

	BTN_DEL     = "⌫"
	BTN_CAPS    = "⇪"
	BTN_SPACE   = "␣"
	BTN_CTRLC   = "^C"
	BTN_ESC     = "⎋"
	BTN_ENTER   = "⏎"
	BTN_CLEAR   = "CLEAR"
	BTN_TAB     = "⇥"
	BTN_HIS_PRE = "PRE"
	BTN_HIS_NXT = "NXT"
)

var (
// TERMINAL_HEIGHT     = int(math.Round(float64(WINDOW_HEIGHT) * TERMINAL_RATIO))
// KEYBOARD_HEIGHT     = int(math.Round(float64(WINDOW_HEIGHT) * KEYBOARD_RATIO))
// CHAR_WIDTH  int = 12 // 调整字符宽度适应20号字体
// CHAR_HEIGHT int = 24 // 调整字符高度适应20号字体
)

type Cell struct {
	char  string
	width int // 字符显示宽度：1为半角，2为全角
}

type Terminal struct {
	cmd           *exec.Cmd
	pty           *os.File
	oldState      *term.State
	output        []string
	maxLines      int
	mutex         sync.RWMutex
	cursorX       int
	cursorY       int
	screenBuffer  [][]Cell
	screenWidth   int
	screenHeight  int
	escapeBuffer  strings.Builder
	inEscape      bool
	lastBlink     time.Time
	cursorVisible bool
	utf8Buffer    []byte
	// 滚动
	totalBuffer [][]Cell // 完整的缓冲区，保存所有历史内容
	totalLines  int      // 总行数
	viewOffset  int      // 视图偏移量（从总缓冲区的哪一行开始显示）
	maxHistory  int      // 最大历史行数
}

type App struct {
	// 配置
	Cfg *Config
	// 渲染
	window   *sdl.Window
	renderer *sdl.Renderer
	font     *ttf.Font
	// 终端
	terminal *Terminal
	running  bool
	// 虚拟键盘
	selectedRow int
	selectedCol int
	capsLock    bool
	keyBoards   [][]string
	// 物理键盘
	keyMaps *KeyMaps
	// 物理手柄
	gamepad      *sdl.GameController
	backPressed  bool
	startPressed bool
	// 添加摇杆状态跟踪
	lastAxisY    int16
	axisDeadzone int16
}

type KeyMaps struct {
	// Ctrl组合键映射
	ctrlKeys map[sdl.Keycode]string
	// Alt组合键映射
	altKeys map[sdl.Keycode]string
	// 功能键映射
	functionKeys map[sdl.Keycode]string
	// 普通字符映射 (不带Shift)
	normalKeys map[sdl.Keycode]string
	// Shift字符映射
	shiftKeys map[sdl.Keycode]string
}

func initKeyMaps() *KeyMaps {
	return &KeyMaps{
		ctrlKeys: map[sdl.Keycode]string{
			sdl.K_c: "\x03",    // Ctrl+C (中断)
			sdl.K_d: "\x04",    // Ctrl+D (EOF)
			sdl.K_z: "\x1a",    // Ctrl+Z (挂起)
			sdl.K_l: "clear\n", // Ctrl+L (清屏)
			sdl.K_a: "\x01",    // Ctrl+A (行首)
			sdl.K_e: "\x05",    // Ctrl+E (行尾)
			sdl.K_u: "\x15",    // Ctrl+U (删除到行首)
			sdl.K_k: "\x0b",    // Ctrl+K (删除到行尾)
			sdl.K_w: "\x17",    // Ctrl+W (删除前一个单词)
			sdl.K_r: "\x12",    // Ctrl+R (搜索历史)
		},
		altKeys: map[sdl.Keycode]string{
			sdl.K_b: "\x1bb", // Alt+B (后退一个单词)
			sdl.K_f: "\x1bf", // Alt+F (前进一个单词)
			sdl.K_d: "\x1bd", // Alt+D (删除下一个单词)
		},
		functionKeys: map[sdl.Keycode]string{
			// 基本控制键
			sdl.K_RETURN:    "\n",
			sdl.K_KP_ENTER:  "\n",
			sdl.K_BACKSPACE: "\b",
			sdl.K_DELETE:    "\x1b[3~",
			sdl.K_TAB:       "\t",
			sdl.K_ESCAPE:    "\x1b",
			// 方向键
			sdl.K_UP:    "\x1b[A",
			sdl.K_DOWN:  "\x1b[B",
			sdl.K_RIGHT: "\x1b[C",
			sdl.K_LEFT:  "\x1b[D",

			// Home/End/Page键
			sdl.K_HOME:     "\x1b[H",
			sdl.K_END:      "\x1b[F",
			sdl.K_PAGEUP:   "\x1b[5~",
			sdl.K_PAGEDOWN: "\x1b[6~",
			sdl.K_INSERT:   "\x1b[2~",

			// F功能键
			sdl.K_F1:  "\x1bOP",
			sdl.K_F2:  "\x1bOQ",
			sdl.K_F3:  "\x1bOR",
			sdl.K_F4:  "\x1bOS",
			sdl.K_F5:  "\x1b[15~",
			sdl.K_F6:  "\x1b[17~",
			sdl.K_F7:  "\x1b[18~",
			sdl.K_F8:  "\x1b[19~",
			sdl.K_F9:  "\x1b[20~",
			sdl.K_F10: "\x1b[21~",
			sdl.K_F11: "\x1b[23~",
			sdl.K_F12: "\x1b[24~",
		},
		normalKeys: map[sdl.Keycode]string{
			// 数字
			sdl.K_0: "0", sdl.K_1: "1", sdl.K_2: "2", sdl.K_3: "3", sdl.K_4: "4",
			sdl.K_5: "5", sdl.K_6: "6", sdl.K_7: "7", sdl.K_8: "8", sdl.K_9: "9",

			// 字母 (小写)
			sdl.K_a: "a", sdl.K_b: "b", sdl.K_c: "c", sdl.K_d: "d", sdl.K_e: "e",
			sdl.K_f: "f", sdl.K_g: "g", sdl.K_h: "h", sdl.K_i: "i", sdl.K_j: "j",
			sdl.K_k: "k", sdl.K_l: "l", sdl.K_m: "m", sdl.K_n: "n", sdl.K_o: "o",
			sdl.K_p: "p", sdl.K_q: "q", sdl.K_r: "r", sdl.K_s: "s", sdl.K_t: "t",
			sdl.K_u: "u", sdl.K_v: "v", sdl.K_w: "w", sdl.K_x: "x", sdl.K_y: "y",
			sdl.K_z: "z",

			// 符号
			sdl.K_SPACE:        " ",
			sdl.K_MINUS:        "-",
			sdl.K_EQUALS:       "=",
			sdl.K_LEFTBRACKET:  "[",
			sdl.K_RIGHTBRACKET: "]",
			sdl.K_BACKSLASH:    "\\",
			sdl.K_SEMICOLON:    ";",
			sdl.K_QUOTE:        "'",
			sdl.K_BACKQUOTE:    "`",
			sdl.K_COMMA:        ",",
			sdl.K_PERIOD:       ".",
			sdl.K_SLASH:        "/",
			// 小键盘
			sdl.K_KP_0: "0", sdl.K_KP_1: "1", sdl.K_KP_2: "2", sdl.K_KP_3: "3",
			sdl.K_KP_4: "4", sdl.K_KP_5: "5", sdl.K_KP_6: "6", sdl.K_KP_7: "7",
			sdl.K_KP_8: "8", sdl.K_KP_9: "9", sdl.K_KP_PERIOD: ".",
			sdl.K_KP_DIVIDE: "/", sdl.K_KP_MULTIPLY: "*",
			sdl.K_KP_MINUS: "-", sdl.K_KP_PLUS: "+",
		},
		shiftKeys: map[sdl.Keycode]string{
			// 数字变符号
			sdl.K_0: ")", sdl.K_1: "!", sdl.K_2: "@", sdl.K_3: "#", sdl.K_4: "$",
			sdl.K_5: "%", sdl.K_6: "^", sdl.K_7: "&", sdl.K_8: "*", sdl.K_9: "(",
			// 字母 (大写)
			sdl.K_a: "A", sdl.K_b: "B", sdl.K_c: "C", sdl.K_d: "D", sdl.K_e: "E",
			sdl.K_f: "F", sdl.K_g: "G", sdl.K_h: "H", sdl.K_i: "I", sdl.K_j: "J",
			sdl.K_k: "K", sdl.K_l: "L", sdl.K_m: "M", sdl.K_n: "N", sdl.K_o: "O",
			sdl.K_p: "P", sdl.K_q: "Q", sdl.K_r: "R", sdl.K_s: "S", sdl.K_t: "T",
			sdl.K_u: "U", sdl.K_v: "V", sdl.K_w: "W", sdl.K_x: "X", sdl.K_y: "Y",
			sdl.K_z: "Z",
			// 符号变换
			sdl.K_MINUS:        "_",
			sdl.K_EQUALS:       "+",
			sdl.K_LEFTBRACKET:  "{",
			sdl.K_RIGHTBRACKET: "}",
			sdl.K_BACKSLASH:    "|",
			sdl.K_SEMICOLON:    ":",
			sdl.K_QUOTE:        "\"",
			sdl.K_BACKQUOTE:    "~",
			sdl.K_COMMA:        "<",
			sdl.K_PERIOD:       ">",
			sdl.K_SLASH:        "?",
		},
	}
}

// getCharWidth 返回字符的显示宽度
func getCharWidth(char string) int {
	if len(char) == 0 {
		return 0
	}

	// 获取第一个rune
	r, _ := utf8.DecodeRuneInString(char)

	// 简单的字符宽度判断
	// 中文字符范围
	if r >= 0x4e00 && r <= 0x9fff {
		return 2
	}
	// 中文标点符号
	if r >= 0x3000 && r <= 0x303f {
		return 2
	}
	// 全角字符
	if r >= 0xff00 && r <= 0xffef {
		return 2
	}
	// 其他一些常见的宽字符
	if r >= 0x1100 && r <= 0x11ff { // 韩文字母
		return 2
	}
	if r >= 0x2e80 && r <= 0x2eff { // CJK 部首补充
		return 2
	}
	if r >= 0x2f00 && r <= 0x2fdf { // 康熙部首
		return 2
	}
	if r >= 0x3100 && r <= 0x312f { // 注音符号
		return 2
	}
	if r >= 0x3200 && r <= 0x32ff { // 带圈字符
		return 2
	}
	if r >= 0x3400 && r <= 0x4dbf { // CJK 扩展A
		return 2
	}
	if r >= 0xac00 && r <= 0xd7af { // 韩文音节
		return 2
	}
	if r >= 0xf900 && r <= 0xfaff { // CJK 兼容汉字
		return 2
	}

	return 1 // 默认半角
}

func NewTerminal(screenWidth, screenHeight int) (*Terminal, error) {
	cmd := exec.Command("bash", "--norc", "--noprofile", "-i")
	maxHistory := 1000 // 保存1000行历史

	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"LANG=zh_CN.UTF-8",
		"LC_ALL=zh_CN.UTF-8",
		fmt.Sprintf("COLUMNS=%d", screenWidth),
		fmt.Sprintf("LINES=%d", screenHeight),
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("启动 pty 失败: %v", err)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("设置终端状态失败: %v", err)
	}

	terminal := &Terminal{
		cmd:           cmd,
		pty:           ptmx,
		oldState:      oldState,
		output:        make([]string, 0),
		maxLines:      screenHeight,
		screenWidth:   screenWidth,
		screenHeight:  screenHeight,
		screenBuffer:  make([][]Cell, screenHeight),
		totalBuffer:   make([][]Cell, maxHistory),
		totalLines:    0,
		viewOffset:    0,
		maxHistory:    maxHistory,
		lastBlink:     time.Now(),
		cursorVisible: true,
		utf8Buffer:    make([]byte, 0, 4),
	}

	// 初始化屏幕缓冲区
	for i := range terminal.screenBuffer {
		terminal.screenBuffer[i] = make([]Cell, screenWidth)
		for j := range terminal.screenBuffer[i] {
			terminal.screenBuffer[i][j] = Cell{char: " ", width: 1}
		}
	}

	// 初始化总缓冲区
	for i := range terminal.totalBuffer {
		terminal.totalBuffer[i] = make([]Cell, screenWidth)
		for j := range terminal.totalBuffer[i] {
			terminal.totalBuffer[i][j] = Cell{char: " ", width: 1}
		}
	}

	winSize := &pty.Winsize{
		Rows: uint16(screenHeight),
		Cols: uint16(screenWidth),
	}
	if err := pty.Setsize(ptmx, winSize); err != nil {
		fmt.Printf("设置窗口大小失败: %v\n", err)
	}

	go terminal.readOutput()
	return terminal, nil
}

// 修改 scrollUp 方法，同时更新总缓冲区
func (t *Terminal) scrollUp() {
	// 如果总缓冲区已满，移除最老的一行
	if t.totalLines >= t.maxHistory {
		// 向上移动所有行
		for y := 0; y < t.maxHistory-1; y++ {
			copy(t.totalBuffer[y], t.totalBuffer[y+1])
		}
		// 清空最后一行
		for x := 0; x < t.screenWidth; x++ {
			t.totalBuffer[t.maxHistory-1][x] = Cell{char: " ", width: 1}
		}
	} else {
		t.totalLines++
	}

	// 将当前屏幕的第一行保存到总缓冲区
	targetLine := min(t.totalLines-1, t.maxHistory-1)
	if targetLine >= 0 {
		copy(t.totalBuffer[targetLine], t.screenBuffer[0])
	}

	// 屏幕缓冲区向上滚动
	for y := 0; y < t.screenHeight-1; y++ {
		copy(t.screenBuffer[y], t.screenBuffer[y+1])
	}
	for x := 0; x < t.screenWidth; x++ {
		t.screenBuffer[t.screenHeight-1][x] = Cell{char: " ", width: 1}
	}
	t.cursorY = t.screenHeight - 1

	// 自动调整视图偏移量，保持显示最新内容
	maxOffset := max(0, t.totalLines-1)
	if t.viewOffset > maxOffset {
		t.viewOffset = maxOffset
	}
}

// 添加滚动控制方法
func (t *Terminal) ScrollView(delta int) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	oldOffset := t.viewOffset
	t.viewOffset += delta

	// 限制滚动范围
	maxOffset := max(0, t.totalLines-1)
	if t.viewOffset < 0 {
		t.viewOffset = 0
	} else if t.viewOffset > maxOffset {
		t.viewOffset = maxOffset
	}

	// 如果偏移量发生变化，更新显示
	if t.viewOffset != oldOffset {
		t.updateDisplayBuffer()
	}
}

// 添加更新显示缓冲区的方法
func (t *Terminal) updateDisplayBuffer() {
	// 如果没有历史内容或者显示最新内容，直接返回
	if t.viewOffset == 0 || t.totalLines == 0 {
		return
	}

	// 计算要显示的历史内容范围
	startLine := max(0, t.totalLines-t.viewOffset-t.screenHeight)

	// 更新屏幕缓冲区，显示历史内容
	for y := 0; y < t.screenHeight; y++ {
		historyLine := startLine + y
		if historyLine >= 0 && historyLine < t.totalLines && historyLine < t.maxHistory {
			copy(t.screenBuffer[y], t.totalBuffer[historyLine])
		} else {
			// 如果没有历史内容，显示空行
			for x := 0; x < t.screenWidth; x++ {
				t.screenBuffer[y][x] = Cell{char: " ", width: 1}
			}
		}
	}
}

func (t *Terminal) readOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := t.pty.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("读取终端输出错误: %v\n", err)
			}
			break
		}
		for i := 0; i < n; i++ {
			t.processByte(buf[i])
		}
		t.updateOutput()
	}
}

func (t *Terminal) processByte(b byte) {
	// 处理UTF-8多字节字符
	t.utf8Buffer = append(t.utf8Buffer, b)

	// 检查是否是完整的UTF-8字符
	if utf8.Valid(t.utf8Buffer) {
		char := string(t.utf8Buffer)
		t.utf8Buffer = t.utf8Buffer[:0] // 清空缓冲区

		// 处理完整的UTF-8字符
		t.processChar(char)
		return
	}

	// 如果缓冲区太长，说明不是有效的UTF-8序列，处理单个字节
	if len(t.utf8Buffer) > 4 {
		// 处理第一个字节作为单个字符
		firstByte := t.utf8Buffer[0]
		t.utf8Buffer = t.utf8Buffer[1:]
		t.processChar(string(firstByte))
	}
}

func (t *Terminal) processChar(char string) {
	if len(char) == 1 {
		b := char[0]
		if t.inEscape {
			t.escapeBuffer.WriteByte(b)
			if t.isEscapeComplete(b) {
				t.processEscapeSequence(t.escapeBuffer.String())
				t.escapeBuffer.Reset()
				t.inEscape = false
			}
			return
		}

		switch b {
		case 27: // ESC
			t.inEscape = true
			t.escapeBuffer.WriteByte(b)
			return
		case '\n':
			t.cursorX = 0
			t.cursorY++
			if t.cursorY >= t.screenHeight {
				t.scrollUp()
			}
			return
		case '\b':
			t.handleBackspace()
			return
		case '\r':
			t.cursorX = 0
			return
		case '\t':
			nextTab := ((t.cursorX / 8) + 1) * 8
			for t.cursorX < nextTab && t.cursorX < t.screenWidth {
				t.screenBuffer[t.cursorY][t.cursorX] = Cell{char: " ", width: 1}
				t.cursorX++
			}
			return
		}
	}

	// 处理可显示字符（包括UTF-8字符）
	// 关键修复：明确排除退格字符和其他控制字符
	if char != "\x00" && char != "\x7f" && char != "\b" && !t.inEscape && isPrintableChar(char) {
		charWidth := getCharWidth(char)

		// 检查是否有足够空间显示该字符
		if t.cursorX+charWidth > t.screenWidth {
			// 换行
			t.cursorX = 0
			t.cursorY++
			if t.cursorY >= t.screenHeight {
				t.scrollUp()
			}
		}

		if t.cursorY < t.screenHeight {
			// 写入字符
			t.screenBuffer[t.cursorY][t.cursorX] = Cell{char: char, width: charWidth}

			// 如果是宽字符，需要在下一个位置标记为占位符
			if charWidth == 2 && t.cursorX+1 < t.screenWidth {
				t.screenBuffer[t.cursorY][t.cursorX+1] = Cell{char: "", width: 0} // 占位符
			}

			t.cursorX += charWidth
		}
	}
}

func (t *Terminal) isEscapeComplete(b byte) bool {
	escSeq := t.escapeBuffer.String()
	if len(escSeq) < 2 {
		return false
	}

	if strings.HasPrefix(escSeq, "\x1b[") {
		return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
	}

	return len(escSeq) >= 2
}

func isPrintableChar(char string) bool {
	if len(char) == 0 {
		return false
	}

	// 对于单字节字符，检查ASCII范围
	if len(char) == 1 {
		b := char[0]
		// 排除所有控制字符 (0-31 和 127)
		if b < 32 || b == 127 {
			return false
		}
		return true
	}

	// 对于多字节字符（UTF-8），获取第一个rune
	r, _ := utf8.DecodeRuneInString(char)

	// 排除Unicode控制字符
	if r < 32 || (r >= 127 && r <= 159) {
		return false
	}

	// 排除特殊的Unicode控制字符
	if r == 0xFEFF || // BOM
		(r >= 0x200B && r <= 0x200F) || // Zero-width spaces
		(r >= 0x2028 && r <= 0x2029) || // Line/Paragraph separators
		(r >= 0xE000 && r <= 0xF8FF) { // Private use area
		return false
	}

	return true
}

func (t *Terminal) handleBackspace() {
	if t.cursorX > 0 {
		targetX := t.cursorX - 1

		// 检查要删除位置的字符类型
		if targetX >= 0 && targetX < t.screenWidth {
			targetCell := t.screenBuffer[t.cursorY][targetX]
			if int(targetCell.width) == 2 {
				// 删除宽字符（如中文）
				t.screenBuffer[t.cursorY][targetX] = Cell{char: " ", width: 1}
				// 清除占位符
				if targetX+1 < t.screenWidth {
					t.screenBuffer[t.cursorY][targetX+1] = Cell{char: " ", width: 1}
				}
				t.cursorX = targetX
			} else if targetCell.width == 0 {
				// 这是宽字符的占位符，需要找到并删除真正的宽字符
				realCharX := targetX - 1
				if realCharX >= 0 && t.screenBuffer[t.cursorY][realCharX].width == 2 {
					t.screenBuffer[t.cursorY][realCharX] = Cell{char: " ", width: 1}
					t.screenBuffer[t.cursorY][targetX] = Cell{char: " ", width: 1}
					t.cursorX = realCharX
				} else {
					// 异常情况，直接清除
					t.screenBuffer[t.cursorY][targetX] = Cell{char: " ", width: 1}
					t.cursorX = targetX
				}
			} else {
				// 普通字符（width == 1）
				t.screenBuffer[t.cursorY][targetX] = Cell{char: " ", width: 1}
				t.cursorX = targetX
			}
		}
	} else if t.cursorY > 0 {
		// 光标在行首，移动到上一行末尾
		t.cursorY--
		// 找到上一行最后一个非空字符的位置
		t.cursorX = t.screenWidth - 1
		for t.cursorX >= 0 && (t.screenBuffer[t.cursorY][t.cursorX].char == " " || t.screenBuffer[t.cursorY][t.cursorX].width == 0) {
			t.cursorX--
		}
		// 移动到该字符后面
		if t.cursorX >= 0 {
			if t.screenBuffer[t.cursorY][t.cursorX].width == 2 {
				t.cursorX += 2 // 跳过宽字符
			} else {
				t.cursorX += 1
			}
		} else {
			t.cursorX = 0 // 上一行为空
		}
		// 确保不超出边界
		if t.cursorX >= t.screenWidth {
			t.cursorX = t.screenWidth - 1
		}
	}
	// 如果 cursorX == 0 && cursorY == 0，什么都不做
}

func (t *Terminal) processEscapeSequence(seq string) {
	if len(seq) < 2 {
		return
	}

	if strings.HasPrefix(seq, "\x1b[") {
		params := seq[2 : len(seq)-1]
		cmd := seq[len(seq)-1]

		switch cmd {
		case 'H', 'f':
			t.setCursorPosition(params)
		case 'A':
			if n := t.parseNumber(params, 1); n > 0 {
				t.cursorY = max(0, t.cursorY-n)
			}
		case 'B':
			if n := t.parseNumber(params, 1); n > 0 {
				t.cursorY = min(t.screenHeight-1, t.cursorY+n)
			}
		case 'C':
			if n := t.parseNumber(params, 1); n > 0 {
				t.cursorX = min(t.screenWidth-1, t.cursorX+n)
			}
		case 'D':
			if n := t.parseNumber(params, 1); n > 0 {
				t.cursorX = max(0, t.cursorX-n)
			}
		case 'J':
			t.clearScreen(params)
		case 'K':
			t.clearLine(params)
		case 'm':
			// 忽略颜色设置
		}
	}
}

func (t *Terminal) parseNumber(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return defaultVal
}

func (t *Terminal) setCursorPosition(params string) {
	parts := strings.Split(params, ";")
	row := t.parseNumber(parts[0], 1) - 1
	col := 0
	if len(parts) > 1 {
		col = t.parseNumber(parts[1], 1) - 1
	}

	t.cursorY = max(0, min(t.screenHeight-1, row))
	t.cursorX = max(0, min(t.screenWidth-1, col))
}

func (t *Terminal) clearScreen(params string) {
	n := t.parseNumber(params, 0)
	switch n {
	case 0:
		for y := t.cursorY; y < t.screenHeight; y++ {
			startX := 0
			if y == t.cursorY {
				startX = t.cursorX
			}
			for x := startX; x < t.screenWidth; x++ {
				t.screenBuffer[y][x] = Cell{char: " ", width: 1}
			}
		}
	case 1:
		for y := 0; y <= t.cursorY; y++ {
			endX := t.screenWidth
			if y == t.cursorY {
				endX = t.cursorX + 1
			}
			for x := 0; x < endX; x++ {
				t.screenBuffer[y][x] = Cell{char: " ", width: 1}
			}
		}
	case 2:
		for y := 0; y < t.screenHeight; y++ {
			for x := 0; x < t.screenWidth; x++ {
				t.screenBuffer[y][x] = Cell{char: " ", width: 1}
			}
		}
		t.cursorX = 0
		t.cursorY = 0
	}
}

func (t *Terminal) clearLine(params string) {
	n := t.parseNumber(params, 0)
	switch n {
	case 0:
		for x := t.cursorX; x < t.screenWidth; x++ {
			t.screenBuffer[t.cursorY][x] = Cell{char: " ", width: 1}
		}
	case 1:
		for x := 0; x <= t.cursorX; x++ {
			t.screenBuffer[t.cursorY][x] = Cell{char: " ", width: 1}
		}
	case 2:
		for x := 0; x < t.screenWidth; x++ {
			t.screenBuffer[t.cursorY][x] = Cell{char: " ", width: 1}
		}
	}
}

func (t *Terminal) updateOutput() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.output = make([]string, t.screenHeight)
	for y := 0; y < t.screenHeight; y++ {
		var line strings.Builder
		for x := 0; x < t.screenWidth; x++ {
			line.WriteString(t.screenBuffer[y][x].char)
		}
		t.output[y] = line.String()
	}
}

func (t *Terminal) Close() {
	if t.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), t.oldState)
	}
	if t.pty != nil {
		t.pty.Close()
	}
	if t.cmd != nil {
		t.cmd.Process.Kill()
	}
}

func NewApp() (*App, error) {
	// step0. init cfg
	cfg, err := func() (*Config, error) {
		var config Config
		pwd, err := os.Getwd()
		if err != nil {
			return &config, err
		}
		file, err := os.Open(pwd + "/cfg.json")
		if err != nil {
			return &config, fmt.Errorf("open cfg faied: %w", err)
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&config); err != nil {
			return &config, fmt.Errorf("marshal json failed: %w", err)
		}
		if config.Window_Width <= 0 || config.Window_Height <= 0 {
			config.Window_Width = 640
			config.Window_Height = 480
		}
		config.terminal_height = int(math.Round(float64(config.Window_Height) * config.TerminalRatio))
		config.keyboard_height = int(math.Round(float64(config.Window_Height) * config.KeyboardRatio))
		config.char_height = 24
		config.char_width = 12
		return &config, nil
	}()
	if err != nil {
		return nil, fmt.Errorf("init cfg failed: %v", err)
	}
	// step1. init sdl
	err = sdl.Init(sdl.INIT_VIDEO | sdl.INIT_GAMECONTROLLER)
	if err != nil {
		return nil, fmt.Errorf("init SDL2 failed: %v", err)
	}
	// step2. init game controller
	gamepad, err := func() (*sdl.GameController, error) {
		numJoysticks := sdl.NumJoysticks()
		for i := 0; i < numJoysticks; i++ {
			if sdl.IsGameController(i) {
				gamepad := sdl.GameControllerOpen(i)
				if gamepad != nil {
					return gamepad, nil
				}
			}
		}
		return nil, fmt.Errorf("init game controller failed, none of any controller found")
	}()
	if err != nil {
		return nil, err
	}
	// step3. init ttf
	err = ttf.Init()
	if err != nil {
		return nil, fmt.Errorf("init ttf failed: %v", err)
	}
	font, err := ttf.OpenFont(filepath.Join(func() string {
		pwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		return pwd
	}(), cfg.Font), cfg.FontSize)
	if err != nil {
		return nil, err
	}
	// step4. init window
	window, err := sdl.CreateWindow(
		"VTerm",
		sdl.WINDOWPOS_UNDEFINED,
		sdl.WINDOWPOS_UNDEFINED,
		int32(cfg.Window_Width),
		int32(cfg.Window_Height),
		sdl.WINDOW_SHOWN,
	)
	if err != nil {
		return nil, fmt.Errorf("init window failed: %v", err)
	}
	// step5. init renderer
	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED)
	if err != nil {
		return nil, fmt.Errorf("init SDL2 renderer failed: %v", err)
	}
	// step6. init termimal comphonent
	terminal, err := NewTerminal(cfg.Window_Width/cfg.char_width, cfg.terminal_height/cfg.char_height)
	if err != nil {
		return nil, fmt.Errorf("init terminal comphonent failed: %v", err)
	}
	// step7. build app
	app := &App{
		Cfg:         cfg,
		window:      window,
		renderer:    renderer,
		font:        font,
		terminal:    terminal,
		running:     true,
		selectedRow: 4,
		selectedCol: 0,
		keyBoards: [][]string{
			{"~", "!", "@", "#", "$", "%", "^", "&", "*", "?"},
			{"-", "+", ",", ";", ":", "/", `\`, ".", "|", BTN_DEL},
			{"(", ")", ">", "<", ">>", "<<", "[", "]", "{", "}"},
			{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"},
			{"q", "w", "e", "r", "t", "y", "u", "i", "o", "p"},
			{"a", "s", "d", "f", "g", "h", "j", "k", "l", BTN_CAPS},
			{"z", "x", "c", "v", "b", "n", "m", BTN_SPACE, BTN_CTRLC, BTN_ENTER},
		},
		keyMaps:      initKeyMaps(),
		gamepad:      gamepad,
		backPressed:  false,
		startPressed: false,
		lastAxisY:    0,
		axisDeadzone: 8000, // 设置死区阈值
	}
	return app, nil
}

// 在 handleInput 函数中添加键盘事件处理
func (a *App) handleInput() {
	for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
		switch e := event.(type) {
		case *sdl.QuitEvent:
			a.running = false
		case *sdl.KeyboardEvent:
			a.handleKeyboard(e)
		case *sdl.ControllerButtonEvent:
			a.handleGamepadButton(e)
		case *sdl.ControllerAxisEvent:
			a.handleGamepadAxis(e)
		}
	}
}

// 实体键盘输入处理函数
func (a *App) handleKeyboard(e *sdl.KeyboardEvent) {
	if e.Type != sdl.KEYDOWN || a.terminal.pty == nil {
		return
	}
	key := e.Keysym.Sym
	mod := e.Keysym.Mod
	if mod&sdl.KMOD_CTRL != 0 {
		if sequence, exists := a.keyMaps.ctrlKeys[key]; exists {
			a.terminal.pty.WriteString(sequence)
			return
		}
	} else if mod&sdl.KMOD_ALT != 0 {
		if sequence, exists := a.keyMaps.altKeys[key]; exists {
			a.terminal.pty.WriteString(sequence)
			return
		}
	} else if sequence, exists := a.keyMaps.functionKeys[key]; exists {
		a.terminal.pty.WriteString(sequence)
		return
	} else {
		shifted := mod&sdl.KMOD_SHIFT != 0
		capsLock := mod&sdl.KMOD_CAPS != 0
		var char string
		var exists bool
		if shifted {
			char, exists = a.keyMaps.shiftKeys[key]
		} else {
			char, exists = a.keyMaps.normalKeys[key]
		}
		// 处理大写锁定 (只影响字母)
		if exists && !shifted && capsLock && key >= sdl.K_a && key <= sdl.K_z {
			char = a.keyMaps.shiftKeys[key] // 获取大写字母
		}
		if exists {
			a.terminal.pty.WriteString(char)
		}
	}
}
func (a *App) handleGamepadAxis(e *sdl.ControllerAxisEvent) {
	// 只处理左摇杆的Y轴
	if e.Axis == sdl.CONTROLLER_AXIS_LEFTY {
		// 设置死区，避免漂移
		if int16(math.Abs(float64(e.Value))) < a.axisDeadzone {
			a.lastAxisY = 0
			return
		}

		// 检测摇杆方向变化，避免连续滚动
		currentDirection := int16(0)
		if e.Value < -a.axisDeadzone {
			currentDirection = -1 // 向上
		} else if e.Value > a.axisDeadzone {
			currentDirection = 1 // 向下
		}

		lastDirection := int16(0)
		if a.lastAxisY < -a.axisDeadzone {
			lastDirection = -1
		} else if a.lastAxisY > a.axisDeadzone {
			lastDirection = 1
		}

		// 只在方向改变时处理
		if currentDirection != lastDirection && currentDirection != 0 {
			if currentDirection < 0 {
				// 摇杆向上 -> 显示内容下移（显示更早的历史）
				a.terminal.ScrollView(3) // 一次滚动3行
			} else {
				// 摇杆向下 -> 显示内容上移（显示更新的内容）
				a.terminal.ScrollView(-3) // 一次滚动3行
			}
		}

		a.lastAxisY = e.Value
	}
}

func (a *App) handleGamepadButton(e *sdl.ControllerButtonEvent) {
	if e.Type == uint32(sdl.CONTROLLERBUTTONDOWN) {
		switch e.Button {
		case sdl.CONTROLLER_BUTTON_DPAD_UP:
			a.DealWithMove(-1, 0)
		case sdl.CONTROLLER_BUTTON_DPAD_DOWN:
			a.DealWithMove(1, 0)
		case sdl.CONTROLLER_BUTTON_DPAD_LEFT:
			a.DealWithMove(0, -1)
		case sdl.CONTROLLER_BUTTON_DPAD_RIGHT:
			a.DealWithMove(0, 1)
		case sdl.CONTROLLER_BUTTON_LEFTSHOULDER:
			a.DealwithInput(BTN_CLEAR)
		case sdl.CONTROLLER_BUTTON_RIGHTSHOULDER:
			a.DealwithInput(BTN_ENTER)
		case sdl.CONTROLLER_BUTTON_X:
			a.DealwithInput(BTN_DEL)
		case sdl.CONTROLLER_BUTTON_B:
			a.DealwithInput("")
		case sdl.CONTROLLER_BUTTON_A:
			a.DealwithInput(BTN_SPACE)
		// case sdl.CONTROLLER_BUTTON_DPAD_LEFT:
		// 	a.DealwithInput(BTN_HIS_PRE)
		// case sdl.CONTROLLER_BUTTON_DPAD_RIGHT:
		// 	a.DealwithInput(BTN_HIS_NXT)
		case sdl.CONTROLLER_BUTTON_BACK:
			a.backPressed = true
		case sdl.CONTROLLER_BUTTON_START:
			a.startPressed = true
		}
		if a.backPressed && a.startPressed {
			a.running = false
		}
	} else if e.Type == sdl.CONTROLLERBUTTONUP {
		switch e.Button {
		case sdl.CONTROLLER_BUTTON_BACK:
			a.backPressed = false
		case sdl.CONTROLLER_BUTTON_START:
			a.startPressed = false
		}
	}
}

func (a *App) DealWithMove(deltaRow, deltaCol int) {
	currentKeyboards := a.keyBoards
	a.selectedRow = (a.selectedRow + deltaRow + len(currentKeyboards)) % len(currentKeyboards)
	maxCol := len(currentKeyboards[a.selectedRow]) - 1
	a.selectedCol = (a.selectedCol + deltaCol + maxCol + 1) % (maxCol + 1)
}

func (a *App) DealwithInput(key string) {
	if a.terminal.pty == nil {
		return
	}
	if key == "" {
		key = a.keyBoards[a.selectedRow][a.selectedCol]
	}
	switch key {
	case BTN_ENTER:
		a.terminal.pty.WriteString("\n")
	case BTN_SPACE:
		a.terminal.pty.WriteString(" ")
	case BTN_DEL:
		a.terminal.pty.WriteString("\b")
	case BTN_CTRLC:
		a.terminal.pty.WriteString("\x03")
	case BTN_ESC:
		a.terminal.pty.WriteString("\x1b")
	case BTN_CLEAR:
		a.terminal.pty.WriteString("clear\n")
	case BTN_HIS_PRE:
		a.terminal.pty.WriteString("\x1b[A")
	case BTN_HIS_NXT:
		a.terminal.pty.WriteString("\x1b[B")
	case BTN_CAPS:
		a.DealWithCapsLock()
	case BTN_TAB:
		a.terminal.pty.WriteString("\t")
	default:
		a.terminal.pty.WriteString(key)
	}
}

func (a *App) DealWithCapsLock() {
	a.capsLock = !a.capsLock
	for i := 4; i < len(a.keyBoards); i++ {
		for j, key := range a.keyBoards[i] {
			if len(key) == 1 {
				if a.capsLock {
					a.keyBoards[i][j] = strings.ToUpper(key)
				} else {
					a.keyBoards[i][j] = strings.ToLower(key)
				}
			} else if key == BTN_CTRLC && a.capsLock {
				a.keyBoards[i][j] = BTN_ESC
			} else if key == BTN_ESC && !a.capsLock {
				a.keyBoards[i][j] = BTN_CTRLC
			}
		}
	}
}

func (a *App) renderTerminal() {
	// 终端背景
	terminalRect := sdl.Rect{X: 0, Y: 0, W: int32(a.Cfg.Window_Width), H: int32(a.Cfg.terminal_height)}
	a.renderer.SetDrawColor(30, 30, 30, 255)
	a.renderer.FillRect(&terminalRect)

	// 终端边框
	a.renderer.SetDrawColor(100, 100, 100, 255)
	a.renderer.DrawRect(&terminalRect)

	a.terminal.mutex.RLock()
	cursorX := a.terminal.cursorX
	cursorY := a.terminal.cursorY

	// 光标闪烁效果
	if time.Since(a.terminal.lastBlink) > 500*time.Millisecond {
		a.terminal.cursorVisible = !a.terminal.cursorVisible
		a.terminal.lastBlink = time.Now()
	}

	// 渲染终端内容
	for y := 0; y < a.terminal.screenHeight; y++ {
		lineY := int32(y * a.Cfg.char_height)
		displayX := 0 // 实际显示位置

		for x := 0; x < a.terminal.screenWidth; x++ {
			cell := a.terminal.screenBuffer[y][x]

			// 跳过占位符（宽字符的第二部分）
			if cell.width == 0 {
				continue
			}

			charX := int32(displayX * a.Cfg.char_width)

			// 渲染字符（现在支持UTF-8）
			if cell.char != " " {
				a.renderText(cell.char, charX, lineY, 220, 220, 220) // 浅灰色文本
			}

			// 渲染光标（闪烁效果）- 调整光标大小适应20号字体
			if y == cursorY && x == cursorX && a.terminal.cursorVisible {
				cursorRect := sdl.Rect{
					X: charX,
					Y: lineY,
					W: 3, // 增加光标宽度适应更大字体
					H: int32(a.Cfg.char_height),
				}
				a.renderer.SetDrawColor(0, 255, 0, 255) // 绿色光标
				a.renderer.FillRect(&cursorRect)
			}

			displayX += cell.width // 根据字符宽度更新显示位置
		}
	}
	a.terminal.mutex.RUnlock()
}

func (a *App) renderKeyboard() {
	keyboardY := a.Cfg.terminal_height
	// 键盘背景
	a.renderer.SetDrawColor(45, 45, 45, 255)
	a.renderer.FillRect(&sdl.Rect{X: 0, Y: int32(keyboardY), W: int32(a.Cfg.Window_Width), H: int32(a.Cfg.keyboard_height)})

	// 区域分隔线
	a.renderer.SetDrawColor(80, 80, 80, 255)
	a.renderer.DrawLine(0, int32(keyboardY), int32(a.Cfg.Window_Width), int32(keyboardY))

	totalRows := len(a.keyBoards)
	margin := 3 // 适当减小间距适应较大字体

	// 调整键盘布局适应20号字体
	availableHeight := a.Cfg.keyboard_height - 16 // 上下各留8px边距
	keyHeight := availableHeight/totalRows - margin

	// 让按键更宽一些，充分利用屏幕宽度
	maxKeysInRow := 10                        // 最多的一行有10个键
	availableWidth := a.Cfg.Window_Width - 16 // 左右各留8px边距
	keyWidth := (availableWidth - (maxKeysInRow-1)*margin) / maxKeysInRow

	// 垂直居中起始位置
	startY := keyboardY + 8 // 固定边距

	for row, keys := range a.keyBoards {
		// 计算该行的起始X位置（水平居中）
		totalRowWidth := len(keys)*keyWidth + (len(keys)-1)*margin
		rowStartX := (a.Cfg.Window_Width - totalRowWidth) / 2

		rowY := startY + row*(keyHeight+margin)

		for col, key := range keys {
			keyX := rowStartX + col*(keyWidth+margin)

			// 按键背景
			keyRect := sdl.Rect{X: int32(keyX), Y: int32(rowY), W: int32(keyWidth), H: int32(keyHeight)}

			// 选中状态
			if row == a.selectedRow && col == a.selectedCol {
				a.renderer.SetDrawColor(70, 130, 180, 255) // 蓝色选中状态
			} else if key == BTN_CAPS && a.capsLock {
				a.renderer.SetDrawColor(220, 20, 60, 255) // 红色大写锁定
			} else {
				a.renderer.SetDrawColor(60, 60, 60, 255) // 普通按键
			}

			// 绘制按键矩形
			a.renderer.FillRect(&keyRect)
			a.renderer.SetDrawColor(100, 100, 100, 255)
			a.renderer.DrawRect(&keyRect)

			// 按键文字 - 改进文字居中，适应20号字体
			textColor := uint8(255)
			if row == a.selectedRow && col == a.selectedCol {
				textColor = 0 // 选中时文字为黑色
			}

			// 更好的文字居中计算，适应20号字体
			textX := int32(keyX + keyWidth/2 - len(key)*4) // 根据20号字体调整文字位置
			textY := int32(rowY + keyHeight/2 - 10)        // 调整垂直居中位置
			a.renderText(key, textX, textY, textColor, textColor, textColor)
		}
	}
}

func (a *App) renderText(text string, x, y int32, r, g, b uint8) {
	if text == "" {
		return
	}
	surface, err := a.font.RenderUTF8Solid(text, sdl.Color{R: r, G: g, B: b, A: 255})
	if err != nil {
		return
	}
	defer surface.Free()
	texture, err := a.renderer.CreateTextureFromSurface(surface)
	if err != nil {
		return
	}
	defer texture.Destroy()
	a.renderer.Copy(texture, nil, &sdl.Rect{X: x, Y: y, W: surface.W, H: surface.H})
}

func (a *App) Close() {
	if a.gamepad != nil {
		a.gamepad.Close()
	}
	if a.terminal != nil {
		a.terminal.Close()
	}
	if a.font != nil {
		a.font.Close()
	}
	if a.renderer != nil {
		a.renderer.Destroy()
	}
	if a.window != nil {
		a.window.Destroy()
	}
	ttf.Quit()
	sdl.Quit()
}

func main() {
	// step1. init app
	app, err := NewApp()
	if err != nil {
		log.Fatalf("init app failed: %v\n", err)
		return
	}
	defer app.Close()
	// step2. block to wait sys signal
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		app.running = false
	}()
	// step3. start msg cycle
	for app.running {
		app.handleInput()
		app.renderer.SetDrawColor(0, 0, 0, 255)
		app.renderer.Clear()

		app.renderTerminal()
		app.renderKeyboard()

		app.renderer.Present()
		sdl.Delay(16)
	}
}
