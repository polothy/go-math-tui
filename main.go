package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	_ "embed"

	cowsay "github.com/Code-Hex/Neo-cowsay/v2"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/gamut"
)

const mathTableEnd = 10

var cowfiles = []string{
	//"alpaca",
	"bud-frogs",
	"default",
	"dragon-and-cow",
	"dragon",
	"elephant",
	//"fox",
	"gopher",
	"hellokitty",
	"kitty",
	"koala",
	//"llama",
	"meow",
	"moose",
	"sheep",
	"small",
	"squirrel",
	"stegosaurus",
	//"sus",
	"turkey",
	"turtle",
}

var (
	//go:embed sounds/level-up-enhancement-8-bit-retro-sound-effect-153002.mp3
	SoundlevelUp []byte
)

type screen int

const (
	screenSplash screen = iota
	screenPlay
	screenLevelUp
	screenEnd
)

type mode int

const (
	modeNone mode = iota
	modeAdd
	modeSub
	modeMul
	modeDiv
)

type problem struct {
	question string
	answer   int
	seen     int
	correct  int
	wrong    int
}

func NewProblem(question string, answer int) problem {
	return problem{question: question, answer: answer}
}

type problems []problem

func NewMulProblems(table int) problems {
	var p problems
	if table == 0 {
		for x := 1; x <= mathTableEnd; x++ {
			p = append(p, NewMulProblems(x)...)
		}
		return p
	}
	for x := 1; x <= mathTableEnd; x++ {
		p = append(p, NewProblem(fmt.Sprintf("%d x %d", table, x), table*x))
	}
	return p
}

func NewDivProblems(table int) problems {
	var p problems
	if table == 0 {
		for y := 1; y <= mathTableEnd; y++ {
			p = append(p, NewDivProblems(y)...)
		}
		return p
	}
	for x := 1; x <= mathTableEnd; x++ {
		p = append(p, NewProblem(fmt.Sprintf("%d / %d", x*table, table), x))
	}
	return p
}

func NewAddProblems(digits int) problems {
	max := pow10(digits)

	// todo this does make dupes, like 1+2 and 2+1, but might not be bad
	var p problems
	for a := 1; a < max; a++ {
		for b := 1; b < max; b++ {
			p = append(p, problem{question: fmt.Sprintf("%d + %d", a, b), answer: a + b})
		}
	}
	return p
}

func NewSubProblems(digits int) problems {
	max := pow10(digits)

	var p problems
	for a := 1; a < max; a++ {
		for b := 1; b < max; b++ {
			if b > a {
				break // Don't do negative answers yet
			}
			p = append(p, problem{question: fmt.Sprintf("%d - %d", a, b), answer: a - b})
		}
	}
	return p
}

// Random selects a random problem, but if the player correctly answers the problem,
// then the problem wont be re-asked until all the other problems are correctly answerd.
// This ensures the player sees all the problems and can retry incorrect ones.
func (p problems) Random() problem {
	low := -1
	for _, prob := range p {
		if low < 0 {
			low = prob.correct
			continue
		}
		if prob.correct < low {
			low = prob.correct
		}
	}
	var candidates problems
	for _, prob := range p {
		if prob.correct == low {
			candidates = append(candidates, prob)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return candidates[rand.Intn(len(candidates))]
}

func (p problems) IndexOf(a problem) int {
	for i, prob := range p {
		if prob.question == a.question {
			return i
		}
	}
	return -1
}

// NewCoach gets a random coach that you have not seen recently
func NewCoach(h map[string]int) string {
	low := -1
	for _, used := range h {
		if low < 0 {
			low = used
			continue
		}
		if used < low {
			low = used
		}
	}
	var candidates []string
	for coach, used := range h {
		if used == low {
			candidates = append(candidates, coach)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return candidates[rand.Intn(len(candidates))]
}

type model struct {
	screen       screen
	mode         mode
	player       string
	digits       int
	table        int
	input        textinput.Model
	feedback     string
	prob         problem
	probs        problems
	coach        string
	coachHist    map[string]int
	level        int
	levelBar     progress.Model
	stopwatch    stopwatch.Model
	windowWidth  int
	windowHeight int
	splashWait   int

	otoContext *oto.Context

	// Stats
	totalRight int
	totalWrong int
	rightMap   map[string]int
	wrongMap   map[string]int
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Enter your name"
	ti.Focus()
	ti.CharLimit = 32
	ti.Width = 20
	ti.PromptStyle = style.Foreground(lipgloss.Color("#1ac500")).Bold(true)
	ti.TextStyle = style
	ti.PlaceholderStyle = style.Foreground(lipgloss.Color("240"))
	ti.CompletionStyle = style.Foreground(lipgloss.Color("240"))
	ti.Cursor.Style = style.Foreground(lipgloss.Color("#F25D94"))

	// TODO https://github.com/charmbracelet/bubbles/pull/543 - once fixed can set EmptyStyle on progress

	coachHist := make(map[string]int)
	for _, coach := range cowfiles {
		coachHist[coach] = 0
	}

	return model{
		screen:     screenSplash,
		splashWait: 3,
		level:      1,
		levelBar:   progress.New(progress.WithDefaultGradient(), progress.WithSpringOptions(15, 0.5), progress.WithoutPercentage()),
		stopwatch:  stopwatch.NewWithInterval(time.Second),
		input:      ti,
		coachHist:  coachHist,
		rightMap:   make(map[string]int),
		wrongMap:   make(map[string]int),
		otoContext: NewOtoContext(),
	}
}

// --- Styling ---
var (
	blends          = gamut.Blends(lipgloss.Color("#F25D94"), lipgloss.Color("#EDFF82"), 50)
	correctBlends   = gamut.Blends(lipgloss.Color("#FF5F87"), lipgloss.Color("#874BFD"), 50)
	incorrectBlends = gamut.Blends(lipgloss.Color("#1ac500"), lipgloss.Color("#3b9be9"), 50)

	bgColor       = lipgloss.Color("#21242a")                                                     // Default background
	style         = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(bgColor) // Base style, so it looks better in light terminals
	appStyle      = style.PaddingTop(1).PaddingLeft(2).PaddingRight(2)                            // Background for whole screen
	dimStyle      = style.Foreground(lipgloss.Color("250")).Faint(true)
	feedbackStyle = style.Background(lipgloss.Color("#ff0000"))

	// titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Underline(true).AlignHorizontal(lipgloss.Center).AlignVertical(lipgloss.Center)
	// questionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F25D94")).Bold(true)
	// splashStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true).Background(lipgloss.Color("57")).Padding(1, 2)
)

func pow10(n int) int {
	out := 1
	for range n {
		out *= 10
	}
	return out
}

func (m model) Init() tea.Cmd {
	var nextCmd tea.Cmd
	if m.splashWait > 0 {
		nextCmd = tea.Tick(time.Second*3, func(time.Time) tea.Msg { return "next" })
	} else {
		nextCmd = func() tea.Msg { return "next" }
	}
	return tea.Batch(textinput.Blink, m.stopwatch.Init(), nextCmd)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.screen = screenEnd
			return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg { return tea.Quit() })
		}
		switch m.screen {
		case screenPlay:
			switch msg.Type {
			case tea.KeyEnter:
				var cmds []tea.Cmd
				val := strings.TrimSpace(m.input.Value())
				if val == "" {
					return m, nil
				}
				m.input.SetValue("") // Reset input

				if m.coach == "" {
					m.coach = NewCoach(m.coachHist)
				}

				lval := strings.ToLower(val)
				if lval == "done" || lval == "quit" || lval == "exit" || lval == "stop" {
					m.screen = screenEnd
					return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg { return tea.Quit() })
				}
				ans, err := strconv.Atoi(val)
				if err == nil {
					if ans == m.prob.answer {
						m.totalRight++
						m.rightMap[m.prob.question]++
						m.prob.correct++

						per := float64(m.totalRight%3) / float64(3)
						if per == 0 {
							per = 1 // Want to show bar as full when they level up!
						}
						level := (m.totalRight / 3) + 1 // Add one because we start at 1
						if level > m.level {
							m.coachHist[m.coach]++
							m.coach = NewCoach(m.coachHist) // After level up, get a new coach!
							m.level = level
							m.screen = screenLevelUp
							cmds = append(cmds, tea.Tick(time.Second*4, func(time.Time) tea.Msg { return "next" }))
							cmds = append(cmds, PlaySoundCmd(m.otoContext, SoundlevelUp))
						}
						cmds = append(cmds, m.levelBar.SetPercent(per))
						m.feedback = rainbow(style, feedbackCoach(m.coach, fmt.Sprintf("Great job! %s = %d ✅", m.prob.question, m.prob.answer)), correctBlends)
						// m.feedback = Lolcatize(feedbackCoach(m.coach, fmt.Sprintf("Great job! %s = %d ✅", m.prob.question, m.prob.answer)))
					} else {
						m.feedback = rainbow(style, feedbackCoach(m.coach, fmt.Sprintf("Nice try! The answer is %s = %d", m.prob.question, m.prob.answer)), incorrectBlends)
						m.totalWrong++
						m.wrongMap[m.prob.question]++
						m.prob.wrong++
					}
					m.prob.seen++
					if i := m.probs.IndexOf(m.prob); i >= 0 {
						m.probs[i] = m.prob
					}
					m.prob = m.probs.Random()
				} else {
					m.feedback = feedbackStyle.Render("Please enter a number!")
				}
				var cmd tea.Cmd
				if len(cmds) > 0 {
					cmd = tea.Batch(cmds...)
				}
				return m, cmd
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}
	case string:
		switch m.screen {
		case screenSplash:
			if msg == "next" {
				m.screen = screenPlay
				m.prob = m.probs.Random()
				m.input.SetValue("")
				m.input.Placeholder = "Your answer"
				m.input.Focus()
				return m, nil
			}
		case screenLevelUp:
			if msg == "next" {
				m.screen = screenPlay
				return m, m.levelBar.SetPercent(0) // Reset level up bar
			}
		}
	case tea.WindowSizeMsg:
		padding := 7
		m.levelBar.Width = msg.Width - padding*2 - 4
		m.windowWidth, m.windowHeight = msg.Width, msg.Height
		return m, nil
	case progress.FrameMsg: // FrameMsg is sent when the progress bar wants to animate itself
		progressModel, cmd := m.levelBar.Update(msg)
		m.levelBar = progressModel.(progress.Model)
		return m, cmd
	default:
		var inputCmd, stopwatchCmd tea.Cmd

		m.input, inputCmd = m.input.Update(msg)             // So it can blink, etc
		m.stopwatch, stopwatchCmd = m.stopwatch.Update(msg) // Update timer

		return m, tea.Batch(inputCmd, stopwatchCmd)
	}
	return m, nil
}

// --- View ---
func (m model) View() string {
	var o string
	switch m.screen {
	case screenSplash:
		o = funMessage(fmt.Sprintf("Welcome, %s!\nLet's play a game :)", m.player), m.windowWidth)
	case screenPlay:
		o = "\n" + rainbow(style.Bold(true), fmt.Sprintf("Question: %s = ?", m.prob.question), blends) +
			"\n\n" + m.input.View() +
			"\n\n" + lipgloss.PlaceHorizontal(m.windowWidth, lipgloss.Center, style.Align(lipgloss.Left).Render(m.feedback)) +
			"\n\n" + rainbow(style.Bold(true), fmt.Sprintf("/// Level %d ", m.level), blends) + m.levelBar.View() +
			"\n\n" + style.Align(lipgloss.Right).Width(m.windowWidth-6).Render(playtime(m.stopwatch.Elapsed())) +
			"\n\n\n" + dimStyle.Render("Psst, press the esc key to stop playing.")

	case screenLevelUp:
		l := `
 _                    _   _    _       _ 
| |                  | | | |  | |     | |
| |     _____   _____| | | |  | |_ __ | |
| |    / _ \ \ / / _ \ | | |  | | '_ \| |
| |___|  __/\ V /  __/ | | |__| | |_) |_|
|______\___| \_/ \___|_|  \____/| .__/(_)
                                | |      
                                |_|      
`
		o = "\n\n" + lipgloss.PlaceHorizontal(m.windowWidth-20, lipgloss.Center, style.Align(lipgloss.Left).Render(Lolcatize(l)), lipgloss.WithWhitespaceBackground(bgColor)) +
			"\n\n" + rainbow(style.Bold(true), fmt.Sprintf("/// Level %d ", m.level), blends) + m.levelBar.View()

	case screenEnd:
		o = funMessage(fmt.Sprintf("Thanks for playing, %s!\n", m.player), m.windowWidth)
	}
	return appStyle.Width(m.windowWidth).Height(m.windowHeight).Render(o)
}

func main() {
	m := parseFlags(initialModel())
	if m.mode == modeNone {
		m = runNewGameForm(m)
	}

	switch m.mode {
	case modeMul:
		m.probs = NewMulProblems(m.table)
	case modeDiv:
		m.probs = NewDivProblems(m.table)
	case modeAdd:
		m.probs = NewAddProblems(m.digits)
	case modeSub:
		m.probs = NewSubProblems(m.digits)
	default:
		panic("forgot to implment problems for new game mode")
	}

	// Uncomment to debug problem generation
	// for _, p := range m.probs {
	// 	fmt.Println(p.question, "=", p.answer)
	// }
	// os.Exit(0)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func parseFlags(m model) model {
	opts := struct {
		Player string
		Digits int
		Table  int
		Mode   int
		Quick  bool
	}{}
	flag.StringVar(&opts.Player, "player", "", "Player name")
	flag.IntVar(&opts.Mode, "mode", 0, fmt.Sprintf("Game mode, add=%d, sub=%d, mul=%d, and div=%d", modeAdd, modeSub, modeMul, modeDiv))
	flag.IntVar(&opts.Digits, "digits", 0, "For sub/add, max number of digits to use")
	flag.IntVar(&opts.Table, "table", 0, "For mul, multiplication table to practice, or zero for all")
	flag.BoolVar(&opts.Quick, "quick", false, "Quickly start")
	flag.Parse()

	if opts.Mode <= 0 || opts.Mode > 4 || opts.Player == "" {
		return m
	}
	m.mode = mode(opts.Mode)
	m.player = opts.Player
	m.digits = opts.Digits
	m.table = opts.Table

	if m.table < 0 {
		m.table = 1
	}
	if m.table > mathTableEnd {
		m.table = mathTableEnd
	}
	if m.digits < 1 {
		m.table = 1
	}
	if m.digits > 3 {
		m.digits = 3
	}
	if opts.Quick {
		m.splashWait = 0
	}
	return m
}

func runNewGameForm(m model) model {
	// Form title
	title := huh.NewNote().Title("WELCOME TO MATH BUDDY!")

	// Player name entry
	playerI := huh.NewInput().Key("player").Value(&m.player).Title("What's your name?").Validate(func(s string) error {
		if s == "" {
			return errors.New("player name is required silly")
		}
		return nil
	})

	// Select type of math problems to solve
	modeI := huh.NewSelect[mode]().
		Key("mode").
		Title("What would you like to practice?").
		Value(&m.mode).
		Options(
			huh.NewOption("Addition", modeAdd),
			huh.NewOption("Subtraction", modeSub),
			huh.NewOption("Multiplication", modeMul),
			huh.NewOption("Division", modeDiv),
		)

	// Based on mode, either select number of digits (sub/add) or which multiplication table to use
	var modeOpt string
	modeOptI := huh.NewInput().Key("modeOpt").Value(&modeOpt).TitleFunc(func() string {
		if m.mode == modeMul || m.mode == modeDiv {
			return fmt.Sprintf("Which table? (1-%d or all)", mathTableEnd)
		}
		return "How many digits max?"
	}, &m.mode).Validate(func(s string) error {
		if (m.mode == modeMul || m.mode == modeDiv) && s == "all" {
			return nil
		}
		num, err := strconv.Atoi(s)
		if err != nil {
			return errors.New("please enter a number")
		}
		if m.mode == modeMul || m.mode == modeDiv {
			if num < 1 || num > mathTableEnd {
				return fmt.Errorf("please enter 1 through %d or all", mathTableEnd)
			}
		} else if num < 1 || num > 3 {
			return errors.New("please enter 1 through 3")
		}
		return nil
	})

	// Display form, full screen
	form := huh.NewForm(huh.NewGroup(title, playerI, modeI, modeOptI)).WithProgramOptions(tea.WithAltScreen())
	if err := form.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	if (m.mode == modeMul || m.mode == modeDiv) && modeOpt == "all" {
		modeOpt = "0"
	}
	num, err := strconv.Atoi(modeOpt)
	if err != nil { // Shouldn't happen, but have to handle it
		fmt.Println("Error: must enter a number - ", err)
		os.Exit(1)
	}
	// Save modeOpt to model
	if m.mode == modeMul || m.mode == modeDiv {
		m.table = num
	} else {
		m.digits = num
	}
	return m
}

func rainbow(base lipgloss.Style, s string, colors []color.Color) string {
	var str string
	for i, ss := range s {
		color, _ := colorful.MakeColor(colors[i%len(colors)])
		str = str + base.Foreground(lipgloss.Color(color.Hex())).Render(string(ss))
	}
	return str
}

func funMessage(message string, windowWidth int) string {
	dialogBoxStyle := style.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#874BFD")).
		BorderBackground(bgColor).
		Padding(1, 0).
		BorderTop(true).
		BorderLeft(true).
		BorderRight(true).
		BorderBottom(true)

	question := style.Width(50).Align(lipgloss.Center).Render(rainbow(style, message, blends))

	return lipgloss.Place(windowWidth, 9,
		lipgloss.Center, lipgloss.Center,
		dialogBoxStyle.Render(question),
	)
}

func feedbackCoach(coach, message string) string {
	say, err := cowsay.Say(
		message,
		cowsay.Type(coach),
		cowsay.BallonWidth(40),
	)
	if err != nil {
		return message
	}
	return say
}

func playtime(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60

	o := fmt.Sprintf("%d second", seconds)
	if seconds > 0 {
		o += "s"
	}
	if minutes == 1 {
		o = "1 minute and " + o
	} else if minutes > 1 {
		o = fmt.Sprintf("%d minutes and %s", minutes, o)
	}
	return rainbow(style.Bold(true), fmt.Sprintf("Playtime: %s!", o), blends)
}

func NewOtoContext() *oto.Context {
	// Prepare an Oto context (this will use your default audio device) that will
	// play all our sounds. Its configuration can't be changed later.

	op := &oto.NewContextOptions{}

	// Usually 44100 or 48000. Other values might cause distortions in Oto
	op.SampleRate = 44100

	// Number of channels (aka locations) to play sounds from. Either 1 or 2.
	// 1 is mono sound, and 2 is stereo (most speakers are stereo).
	op.ChannelCount = 2

	// Format of the source. go-mp3's format is signed 16bit integers.
	op.Format = oto.FormatSignedInt16LE

	// Remember that you should **not** create more than one context
	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		// panic("oto.NewContext failed: " + err.Error())
		return nil
	}
	// It might take a bit for the hardware audio devices to be ready, so we wait on the channel.
	<-readyChan

	return otoCtx
}

func PlaySoundCmd(otoCtx *oto.Context, sound []byte) tea.Cmd {
	return func() tea.Msg {
		if otoCtx == nil {
			return nil
		}
		// Convert the pure bytes into a reader object that can be used with the mp3 decoder
		fileBytesReader := bytes.NewReader(sound)

		// Decode file
		decodedMp3, err := mp3.NewDecoder(fileBytesReader)
		if err != nil {
			return nil
			// panic("mp3.NewDecoder failed: " + err.Error())
		}

		// Create a new 'player' that will handle our sound. Paused by default.
		player := otoCtx.NewPlayer(decodedMp3)

		// Play starts playing the sound and returns without waiting for it (Play() is async).
		player.Play()

		// We can wait for the sound to finish playing using something like this
		for player.IsPlaying() {
			time.Sleep(time.Millisecond)
		}
		return nil // Could return a message like soundFinishedMsg{} if I needed to know when it was done
	}
}

// --- AI generated ---

// Lolcatize is the simple entry point with sensible defaults.
// It returns the coloured string (does not print).
func Lolcatize(s string) string {
	// defaults tuned to match classic lolcat appearance
	return LolcatizeWithConfig(s, spreadDefault, freqDefault, 0.0, true)
}

// Configurable values (tweak to taste)
const (
	spreadDefault = 3.0 // how "wide" the rainbow is (characters per hue sweep)
	freqDefault   = 0.3 // frequency multiplier (bigger -> faster color changes)
)

const twoPI = 2 * math.Pi

// LolcatizeWithConfig colorizes the string s and returns it.
// Parameters:
//   - spread: larger -> slower color change across characters (suggest 1.5..6.0)
//   - freq: frequency multiplier, typical ~0.2..0.6
//   - seed: phase offset (useful to shift the rainbow)
//   - resetPerLine: if true each newline resets the gradient so each line starts fresh
func LolcatizeWithConfig(s string, spread, freq, seed float64, resetPerLine bool) string {
	if spread <= 0 {
		spread = spreadDefault
	}
	if freq <= 0 {
		freq = freqDefault
	}

	var out strings.Builder
	idx := 0 // index used for color progression (resets per-line when requested)

	// Helper that clamps and rounds a float to [0..255]
	clampByte := func(v float64) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return int(math.Round(v))
	}

	for _, r := range s {
		if r == '\n' {
			_, _ = out.WriteRune(r)
			if resetPerLine {
				idx = 0
			} else {
				idx++
			}
			continue
		}

		// Classic lolcat-style RGB waves:
		//   rad = seed + idx/spread * freq * 2π
		//   r = sin(rad + 0) * 127 + 128
		//   g = sin(rad + 2π/3) * 127 + 128
		//   b = sin(rad + 4π/3) * 127 + 128
		rad := seed + (float64(idx)/spread)*freq*twoPI

		red := math.Sin(rad)*127.0 + 128.0
		green := math.Sin(rad+twoPI/3.0)*127.0 + 128.0
		blue := math.Sin(rad+2.0*twoPI/3.0)*127.0 + 128.0

		ri := clampByte(red)
		gi := clampByte(green)
		bi := clampByte(blue)

		hex := fmt.Sprintf("#%02x%02x%02x", ri, gi, bi)
		style := style.Foreground(lipgloss.Color(hex))
		out.WriteString(style.Render(string(r)))

		idx++
	}

	return out.String()
}
