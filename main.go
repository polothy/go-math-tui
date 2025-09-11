package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	// "github.com/NimbleMarkets/ntcharts"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/gamut"
)

const mathTableEnd = 10

var cowfiles = []string{
	"fox",
	"alpaca",
	"dragon",
	"bud-frogs",
	"dragon-and-cow",
	"elephant",
	"hellokitty",
	"kitty",
	"llama",
	"koala",
	"meow",
	"moose",
	"sheep",
	"small",
	"default",
	"stegosaurus",
	"sus",
	"turkey",
	"turtle",
}

type screen int

const (
	screenSplash screen = iota
	screenPlay
	screenEnd
)

type mode int

const (
	modeNone mode = iota
	modeAdd
	modeSub
	modeMul
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
	newTable := func(x, max int) problems {
		p := make(problems, 0, max)
		for y := 1; y <= max; y++ {
			p = append(p, NewProblem(fmt.Sprintf("%d x %d", x, y), x*y))
		}
		return p
	}
	if table == 0 {
		var p problems
		for x := 1; x <= mathTableEnd; x++ {
			x++
			p = append(p, newTable(x, mathTableEnd)...)
		}
		return p
	}
	return newTable(table, mathTableEnd)
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

type model struct {
	screen   screen
	mode     mode
	player   string
	digits   int
	table    int
	input    textinput.Model
	feedback string
	prob     problem
	probs    problems
	coach    string

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

	return model{
		screen:   screenSplash,
		input:    ti,
		rightMap: make(map[string]int),
		wrongMap: make(map[string]int),
	}
}

// --- Styling ---
var (
	blends          = gamut.Blends(lipgloss.Color("#F25D94"), lipgloss.Color("#EDFF82"), 50)
	correctBlends   = gamut.Blends(lipgloss.Color("#FF5F87"), lipgloss.Color("#874BFD"), 50)
	incorrectBlends = gamut.Blends(lipgloss.Color("#fb9700"), lipgloss.Color("#EDFF82"), 50)

	titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Underline(true).AlignHorizontal(lipgloss.Center).AlignVertical(lipgloss.Center)
	questionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	feedbackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
	splashStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true).Background(lipgloss.Color("57")).Padding(1, 2)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
)

// --- Helpers ---
// func makeProblem(m model) problem {
// 	switch m.mode {
// 	case modeAdd:
// 		max := pow10(m.digits)
// 		a, b := rand.Intn(max), rand.Intn(max)
// 		return problem{question: fmt.Sprintf("%d + %d", a, b), answer: a + b}
// 	case modeSub:
// 		max := pow10(m.digits)
// 		a, b := rand.Intn(max), rand.Intn(max)
// 		if a < b {
// 			a, b = b, a
// 		}
// 		return problem{question: fmt.Sprintf("%d - %d", a, b), answer: a - b}
// 	case modeMul:
// 		return m.probs.Random()
// 		// x := m.table
// 		// if x == 0 {
// 		// 	x = rand.Intn(10) + 1
// 		// }
// 		// y := rand.Intn(10) + 1
// 		// return problem{fmt.Sprintf("%d x %d", x, y), x * y}
// 	}
// 	return problem{}
// }

func pow10(n int) int {
	out := 1
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}

// --- Update ---
func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tea.Tick(time.Second*3, func(time.Time) tea.Msg { return "next" }))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.screen = screenEnd
			return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg { return tea.Quit() })
		}
		switch m.screen {
		case screenPlay:
			switch msg.Type {
			case tea.KeyEnter:
				val := strings.TrimSpace(m.input.Value())
				if val == "" {
					return m, nil
				}
				m.input.SetValue("") // Reset input

				if m.coach == "" {
					m.coach = cowfiles[rand.Intn(len(cowfiles))]
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

						if m.totalRight%3 == 0 {
							for range 100 {
								newCoach := cowfiles[rand.Intn(len(cowfiles))]
								if m.coach != newCoach {
									m.coach = newCoach
									break
								}
							}
						}

						m.feedback = rainbow(lipgloss.NewStyle(), feedbackCoach(m.coach, fmt.Sprintf("Great job! %s = %d ✅", m.prob.question, m.prob.answer)), correctBlends)
					} else {
						m.feedback = rainbow(lipgloss.NewStyle(), feedbackCoach(m.coach, fmt.Sprintf("Nice try! The answer is %s = %d", m.prob.question, m.prob.answer)), incorrectBlends)
						m.totalWrong++
						m.wrongMap[m.prob.question]++
						m.prob.wrong++
					}
					m.prob.seen++
					if i := m.probs.IndexOf(m.prob); i >= 0 {
						m.probs[i] = m.prob
					}
					// m.prob = makeProblem(m)
					m.prob = m.probs.Random()
				} else {
					m.feedback = "Please enter a number!"
				}
				return m, nil
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
				// m.prob = makeProblem(m)
				m.prob = m.probs.Random()
				m.input.SetValue("")
				m.input.Placeholder = "Your answer"
				m.input.Focus()
				return m, nil
			}
		}
	}
	return m, nil
}

// --- View ---
func (m model) View() string {
	switch m.screen {
	case screenSplash:
		return funMessage(fmt.Sprintf("Welcome, %s!\nLet's play a game \uee80", m.player))
	case screenPlay:
		return questionStyle.Render(fmt.Sprintf("Question: %s = ?", m.prob.question)) +
			"\n\n" + m.input.View() +
			"\n\n" + m.feedback +
			"\n\n\n" + dimStyle.Render("press esc key to stop playing")
	case screenEnd:
		var out string
		// out = testStyle.Render(fmt.Sprintf("Thanks for playing, %s!\n", m.player))
		// out = rainbow(lipgloss.NewStyle(), fmt.Sprintf("Thanks for playing, %s!\n", m.player), blends)
		out = funMessage(fmt.Sprintf("Thanks for playing, %s!\n", m.player))
		// out += fmt.Sprintf("You got %d right and %d wrong.\n\n", m.totalRight, m.totalWrong)
		// if m.mode == modeMul {
		// 	data := []ntcharts.BarDatum{}
		// 	for q, c := range m.rightMap {
		// 		data = append(data, ntcharts.BarDatum{Name: q, Value: c})
		// 	}
		// 	for q, c := range m.wrongMap {
		// 		data = append(data, ntcharts.BarDatum{Name: q + " (wrong)", Value: c})
		// 	}
		// 	chart := ntcharts.BarChart(data, ntcharts.WithHeight(10))
		// 	out += chart
		// }
		return out
	}
	return ""
}

func main() {
	rand.Seed(time.Now().UnixNano())

	p := tea.NewProgram(runNewGameForm(initialModel()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
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
		)

	// Based on mode, either select number of digits (sub/add) or which multiplication table to use
	var modeOpt string
	modeOptI := huh.NewInput().Key("modeOpt").Value(&modeOpt).TitleFunc(func() string {
		if m.mode == modeMul {
			return fmt.Sprintf("Which table? (1-%d or all)", mathTableEnd)
		}
		return "How many digits max?"
	}, &m.mode).Validate(func(s string) error {
		if m.mode == modeMul && s == "all" {
			return nil
		}
		num, err := strconv.Atoi(s)
		if err != nil {
			return errors.New("please enter a number")
		}
		if m.mode == modeMul {
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
	if m.mode == modeMul && modeOpt == "all" {
		modeOpt = "0"
	}
	num, err := strconv.Atoi(modeOpt)
	if err != nil { // Shouldn't happen, but have to handle it
		fmt.Println("Error: must enter a number - ", err)
		os.Exit(1)
	}
	// Save modeOpt to model
	if m.mode == modeMul {
		m.table = num
		m.probs = NewMulProblems(m.table)
	} else {
		m.digits = num
		if m.mode == modeAdd {
			m.probs = NewAddProblems(m.digits)
		} else {
			m.probs = NewSubProblems(m.digits)
		}
	}
	// fmt.Println("ADD")
	// for _, p := range NewAddProblems(1) {
	// 	fmt.Println(p.question, "=", p.answer)
	// }
	// fmt.Println()
	// fmt.Println()
	// fmt.Println("SUB")
	// for _, p := range NewSubProblems(1) {
	// 	fmt.Println(p.question, "=", p.answer)
	// }
	// fmt.Println()
	// fmt.Println()
	// fmt.Println("MUL")
	// for _, p := range NewMulProblems(3) {
	// 	fmt.Println(p.question, "=", p.answer)
	// }
	// os.Exit(0)
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

func funMessage(message string) string {
	dialogBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#874BFD")).
		Padding(1, 0).
		BorderTop(true).
		BorderLeft(true).
		BorderRight(true).
		BorderBottom(true)

	question := lipgloss.NewStyle().Width(50).Align(lipgloss.Center).Render(rainbow(lipgloss.NewStyle(), message, blends))

	return lipgloss.Place(90, 9,
		lipgloss.Center, lipgloss.Center,
		dialogBoxStyle.Render(question),
	)
}

func feedbackCoach(coach, message string) string {
	cowsay, err := exec.LookPath("cowsay")
	if err != nil {
		return message
	}
	cmd := exec.Command(cowsay, "-f", coach, message)
	o, err := cmd.Output()
	if err != nil {
		return message
	}
	return string(o)
}

func lolcat(a string) string {
	// The base rainbow color scheme. The values are HSL-like.
	var colors = []lipgloss.Color{
		lipgloss.Color("#FF0000"), // Red
		lipgloss.Color("#FF7F00"), // Orange
		lipgloss.Color("#FFFF00"), // Yellow
		lipgloss.Color("#00FF00"), // Green
		lipgloss.Color("#0000FF"), // Blue
		lipgloss.Color("#4B0082"), // Indigo
		lipgloss.Color("#9400D3"), // Violet
	}
	scanner := bufio.NewScanner(bytes.NewBufferString(a))

	colorCount := len(colors)
	colorIndex := 0

	b := strings.Builder{}
	for scanner.Scan() {
		line := scanner.Text()
		for _, r := range line {
			style := lipgloss.NewStyle().Foreground(colors[colorIndex%colorCount])
			b.WriteString(style.Render(string(r)))
			colorIndex++
		}
		b.WriteString("\n")
	}
	return b.String()
}
