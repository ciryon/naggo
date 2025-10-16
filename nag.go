// go mod init nag && go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/gen2brain/beeep
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/common-nighthawk/go-figure"
	"github.com/gen2brain/beeep"
	"github.com/hajimehoshi/oto/v2"
)

type tickMsg time.Time

type model struct {
	remaining         time.Duration
	running           bool
	alarmSounds       []soundClip
	alarmIdx          int
	switchSound       soundClip
	stopSound         soundClip
	adjustSound       soundClip
	alarmActive       bool
	blinkVisible      bool
	lastAlarmPlay     time.Time
	lastBlinkToggle   time.Time
	lastTick          time.Time
	colonVisible      bool
	lastColonToggle   time.Time
	stopPromptVisible bool
}

func initialModel() model {
	sounds := loadSoundSet()
	alarmIdx := sounds.defaultAlarm
	if alarmIdx < 0 || alarmIdx >= len(sounds.alarms) {
		alarmIdx = 0
	}

	return model{
		remaining:         10 * time.Minute,
		running:           false,
		alarmSounds:       sounds.alarms,
		alarmIdx:          alarmIdx,
		switchSound:       sounds.switchClip,
		stopSound:         sounds.stop,
		adjustSound:       sounds.adjust,
		blinkVisible:      true,
		colonVisible:      true,
		stopPromptVisible: false,
	}
}

const (
	alarmRepeatInterval = time.Minute
	blinkInterval       = 250 * time.Millisecond
	tickInterval        = 250 * time.Millisecond
	colonBlinkInterval  = 500 * time.Millisecond
)

func (m *model) activateAlarm(now time.Time) {
	m.alarmActive = true
	m.blinkVisible = true
	m.lastAlarmPlay = now
	m.lastBlinkToggle = now
	m.colonVisible = true
	m.lastColonToggle = time.Time{}
	m.stopPromptVisible = true
}

func (m *model) silenceAlarm() {
	m.alarmActive = false
	m.blinkVisible = true
	m.lastAlarmPlay = time.Time{}
	m.lastBlinkToggle = time.Time{}
	m.colonVisible = true
	m.lastColonToggle = time.Time{}
	m.stopPromptVisible = false
}

func (m model) Init() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		now := time.Time(msg)
		if m.lastTick.IsZero() {
			m.lastTick = now
		}
		delta := now.Sub(m.lastTick)
		if delta < 0 {
			delta = 0
		}
		if m.running && m.remaining > 0 {
			m.remaining -= delta
			if m.lastColonToggle.IsZero() {
				m.lastColonToggle = now
			}
			if now.Sub(m.lastColonToggle) >= colonBlinkInterval {
				m.colonVisible = !m.colonVisible
				m.lastColonToggle = now
			}
			if m.remaining <= 0 {
				m.remaining = 0
				notify("Nag", "Time's up")
				playSound(m.currentAlarm())
				m.activateAlarm(now)
				m.running = false
			}
		} else {
			m.colonVisible = true
			m.lastColonToggle = time.Time{}
		}
		m.lastTick = now

		if m.alarmActive {
			if m.remaining > 0 {
				m.silenceAlarm()
			} else {
				if !m.lastAlarmPlay.IsZero() && now.Sub(m.lastAlarmPlay) >= alarmRepeatInterval {
					playSound(m.currentAlarm())
					m.lastAlarmPlay = now
				}
				if m.lastBlinkToggle.IsZero() {
					m.lastBlinkToggle = now
				}
				if now.Sub(m.lastBlinkToggle) >= blinkInterval {
					m.blinkVisible = !m.blinkVisible
					m.lastBlinkToggle = now
				}
			}
		} else if !m.blinkVisible {
			m.blinkVisible = true
		}
		return m, tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case " ":
			m.running = !m.running
			m.silenceAlarm()
			playSound(m.switchSound)
		case "s":
			m.running = false
			m.silenceAlarm()
			playSound(m.stopSound)
		case "up":
			m.remaining += time.Minute
			m.silenceAlarm()
			playSound(m.adjustSound)
		case "down":
			if m.remaining > time.Minute {
				m.remaining -= time.Minute
			} else if m.remaining > time.Second {
				m.remaining -= time.Second
			} else {
				m.remaining = time.Second
			}
			m.silenceAlarm()
			playSound(m.adjustSound)
		case "right":
			m.remaining += time.Hour
			m.silenceAlarm()
			playSound(m.adjustSound)
		case "left":
			if m.remaining > time.Hour {
				m.remaining -= time.Hour
			} else {
				m.remaining = time.Minute
			}
			m.silenceAlarm()
			playSound(m.adjustSound)
		case "/":
			if len(m.alarmSounds) > 0 {
				m.alarmIdx = (m.alarmIdx + 1) % len(m.alarmSounds)
				playSound(m.adjustSound)
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			minutes := time.Duration(msg.String()[0]-'0') * time.Minute
			m.remaining = minutes
			m.running = false
			m.silenceAlarm()
			playSound(m.switchSound)
		}
	}
	return m, nil
}

func (m model) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("213"))

	borderStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("213")).
		Padding(1, 2)

	countdownStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("45")).
		Bold(true)

	helpStyle := lipgloss.NewStyle().
		Faint(true).
		Width(70)
	soundLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("219")).
		Bold(true)

	remaining := m.remaining
	prefix := ""
	if remaining < 0 {
		prefix = "-"
		remaining = -remaining
	}

	mins := int(remaining / time.Minute)
	secs := int(remaining%time.Minute) / int(time.Second)

	colonVisible := !(m.running && !m.colonVisible)
	countdownText := fmt.Sprintf("%s%02d:%02d", prefix, mins, secs)
	showCountdown := !m.alarmActive || m.blinkVisible
	ascii := renderCountdownFigure(countdownText, showCountdown, colonVisible)
	countdownBlock := borderStyle.Render(countdownStyle.Render(ascii))

	soundLine := fmt.Sprintf("Alarm: %s", soundLabelStyle.Render(m.alarmName()))
	alarmInfo := ""
	if m.alarmActive {
		alarmInfo = "Repeating every 1m – pause to silence"
	}

	help := strings.Join([]string{
		"space start/pause",
		"s stop",
		"up +1m",
		"down -1m",
		"right +1h",
		"left -1h",
		"1-9 set minutes",
		"/ cycle alarm",
		"q quit",
	}, " • ")

	lines := []string{
		titleStyle.Render("Nag Timer"),
		countdownBlock,
	}
	if m.stopPromptVisible {
		stopText := lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true).Render("Press s to stop")
		lines = append(lines, stopText)
	} else if alarmInfo != "" {
		lines = append(lines, alarmInfo)
	}
	lines = append(lines, soundLine)
	lines = append(lines, helpStyle.Render(help))

	return lipgloss.JoinVertical(lipgloss.Left, lines...) + "\n"
}

// Precompute figure glyphs to keep the countdown frame stable.
var (
	figureChars           map[rune][]string
	countdownFigureWidth  int
	countdownFigureHeight int
)

func init() {
	figureChars, countdownFigureHeight = buildFigureChars()
	sample := renderCountdownFigure("-88:88", true, true)
	firstLine := strings.Split(sample, "\n")[0]
	countdownFigureWidth = len(firstLine)
}

func renderCountdownFigure(text string, visible bool, colonVisible bool) string {
	if !visible {
		blank := strings.Repeat(" ", countdownFigureWidth)
		lines := make([]string, countdownFigureHeight)
		for i := range lines {
			lines[i] = blank
		}
		return strings.Join(lines, "\n")
	}

	lines := make([]string, countdownFigureHeight)
	for _, r := range text {
		figLines, ok := figureChars[r]
		if !ok {
			figLines = figureChars[' ']
		}
		for i := 0; i < countdownFigureHeight; i++ {
			segment := figLines[i]
			if r == ':' && !colonVisible {
				segment = strings.Repeat(" ", len(segment))
			}
			lines[i] += segment
		}
	}

	for i := range lines {
		if len(lines[i]) < countdownFigureWidth {
			lines[i] += strings.Repeat(" ", countdownFigureWidth-len(lines[i]))
		}
	}

	return strings.Join(lines, "\n")
}

type soundClip struct {
	name string
	data []byte
}

func buildFigureChars() (map[rune][]string, int) {
	chars := []rune{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', '-', ' '}
	figMap := make(map[rune][]string, len(chars))
	maxHeight := 0
	for _, r := range chars {
		fig := figure.NewFigure(string(r), "", true)
		lines := fig.Slicify()
		if len(lines) == 0 {
			lines = []string{""}
		}
		width := 0
		for _, line := range lines {
			if len(line) > width {
				width = len(line)
			}
		}
		if width == 0 {
			width = 1
		}
		for i := range lines {
			lines[i] = padRight(lines[i], width)
		}
		if len(lines) > maxHeight {
			maxHeight = len(lines)
		}
		figMap[r] = lines
	}

	spaceWidth := 0
	if lines, ok := figMap['0']; ok && len(lines) > 0 {
		spaceWidth = len(lines[0])
	}
	if spaceWidth == 0 {
		spaceWidth = 8
	}

	for r, lines := range figMap {
		width := len(lines[0])
		if len(lines) < maxHeight {
			padded := make([]string, maxHeight)
			copy(padded, lines)
			for i := len(lines); i < maxHeight; i++ {
				padded[i] = strings.Repeat(" ", width)
			}
			figMap[r] = padded
		}
	}

	spaceLines := make([]string, maxHeight)
	for i := range spaceLines {
		spaceLines[i] = strings.Repeat(" ", spaceWidth)
	}
	figMap[' '] = spaceLines

	return figMap, maxHeight
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

type soundSet struct {
	alarms       []soundClip
	defaultAlarm int
	switchClip   soundClip
	stop         soundClip
	adjust       soundClip
}

func (m model) currentAlarm() soundClip {
	if len(m.alarmSounds) == 0 {
		return soundClip{}
	}
	idx := m.alarmIdx % len(m.alarmSounds)
	if idx < 0 {
		idx += len(m.alarmSounds)
	}
	return m.alarmSounds[idx]
}

func (m model) alarmName() string {
	if len(m.alarmSounds) == 0 {
		return "Bell"
	}
	return m.currentAlarm().name
}

func loadSoundSet() soundSet {
	set := soundSet{}

	set.switchClip = generateToneClip("Switch", defaultSwitchTone())
	set.stop = generateToneClip("Stop", defaultStopTone())
	set.adjust = generateToneClip("Rad", defaultAdjustTone())

	alarmDefinitions := []struct {
		base     string
		fallback []toneSegment
	}{
		{base: "5bip", fallback: defaultFiveBipTone()},
		{base: "siren", fallback: defaultSirenTone()},
		{base: "japan", fallback: defaultJapanTone()},
		{base: "quad", fallback: defaultQuadTone()},
		{base: "toot", fallback: defaultTootTone()},
	}

	for idx, def := range alarmDefinitions {
		clip := generateToneClip(prettyName(def.base), def.fallback)
		set.alarms = append(set.alarms, clip)
		if def.base == "siren" {
			set.defaultAlarm = idx
		}
	}

	if len(set.alarms) == 0 {
		set.alarms = append(set.alarms, generateToneClip("Alarm", defaultSirenTone()))
		set.defaultAlarm = 0
	}

	return set
}

type toneSegment struct {
	freq      float64
	duration  time.Duration
	amplitude float64
}

func generateToneClip(name string, segments []toneSegment) soundClip {
	var data []byte
	for _, seg := range segments {
		data = append(data, synthSegment(seg)...)
	}
	return soundClip{
		name: name,
		data: data,
	}
}

func synthSegment(seg toneSegment) []byte {
	if seg.duration <= 0 {
		return nil
	}

	samples := int(float64(audioSampleRate) * seg.duration.Seconds())
	if samples <= 0 {
		return nil
	}

	buf := make([]byte, samples*2)
	if seg.freq <= 0 {
		return buf
	}

	amp := seg.amplitude
	if amp <= 0 {
		amp = 0.6
	}
	if amp > 1 {
		amp = 1
	}

	for i := 0; i < samples; i++ {
		env := envelope(i, samples)
		sample := math.Sin(2 * math.Pi * seg.freq * float64(i) / float64(audioSampleRate))
		val := int16(sample * amp * env * math.MaxInt16)
		binary.LittleEndian.PutUint16(buf[2*i:], uint16(val))
	}

	return buf
}

func envelope(i, total int) float64 {
	if total <= 0 {
		return 0
	}
	attackSamples := int(math.Round(0.005 * float64(audioSampleRate)))
	releaseSamples := attackSamples
	if attackSamples < 1 {
		attackSamples = 1
	}
	if releaseSamples < 1 {
		releaseSamples = 1
	}

	env := 1.0
	if i < attackSamples {
		env = float64(i) / float64(attackSamples)
	}
	if total-i < releaseSamples {
		release := float64(total-i) / float64(releaseSamples)
		if release < env {
			env = release
		}
	}
	if env < 0 {
		env = 0
	}
	if env > 1 {
		env = 1
	}
	return env
}

func defaultSwitchTone() []toneSegment {
	return []toneSegment{
		{freq: 880, duration: 70 * time.Millisecond, amplitude: 0.8},
		{duration: 30 * time.Millisecond},
		{freq: 660, duration: 70 * time.Millisecond, amplitude: 0.7},
	}
}

func defaultStopTone() []toneSegment {
	return []toneSegment{
		{freq: 440, duration: 120 * time.Millisecond, amplitude: 0.7},
		{duration: 40 * time.Millisecond},
		{freq: 330, duration: 160 * time.Millisecond, amplitude: 0.7},
	}
}

func defaultAdjustTone() []toneSegment {
	return []toneSegment{
		{freq: 1200, duration: 90 * time.Millisecond, amplitude: 0.6},
	}
}

func defaultSirenTone() []toneSegment {
	pattern := []toneSegment{
		{freq: 870, duration: 160 * time.Millisecond, amplitude: 0.85},
		{duration: 40 * time.Millisecond},
		{freq: 650, duration: 160 * time.Millisecond, amplitude: 0.85},
		{duration: 40 * time.Millisecond},
	}
	var seq []toneSegment
	for i := 0; i < 4; i++ {
		seq = append(seq, pattern...)
	}
	return seq
}

func defaultJapanTone() []toneSegment {
	return []toneSegment{
		{freq: 523.25, duration: 110 * time.Millisecond, amplitude: 0.7}, // C5
		{duration: 30 * time.Millisecond},
		{freq: 659.25, duration: 110 * time.Millisecond, amplitude: 0.7}, // E5
		{duration: 30 * time.Millisecond},
		{freq: 783.99, duration: 130 * time.Millisecond, amplitude: 0.7}, // G5
		{duration: 160 * time.Millisecond},
	}
}

func defaultQuadTone() []toneSegment {
	return []toneSegment{
		{freq: 740, duration: 80 * time.Millisecond, amplitude: 0.6},
		{duration: 30 * time.Millisecond},
		{freq: 740, duration: 80 * time.Millisecond, amplitude: 0.6},
		{duration: 30 * time.Millisecond},
		{freq: 740, duration: 80 * time.Millisecond, amplitude: 0.6},
		{duration: 30 * time.Millisecond},
		{freq: 740, duration: 80 * time.Millisecond, amplitude: 0.6},
	}
}

func defaultTootTone() []toneSegment {
	return []toneSegment{
		{freq: 330, duration: 150 * time.Millisecond, amplitude: 0.7},
		{duration: 40 * time.Millisecond},
		{freq: 660, duration: 200 * time.Millisecond, amplitude: 0.7},
	}
}

func defaultFiveBipTone() []toneSegment {
	return []toneSegment{
		{freq: 880, duration: 80 * time.Millisecond, amplitude: 0.7},
		{duration: 20 * time.Millisecond},
		{freq: 1046.5, duration: 80 * time.Millisecond, amplitude: 0.7},
		{duration: 20 * time.Millisecond},
		{freq: 1244.5, duration: 90 * time.Millisecond, amplitude: 0.7},
	}
}

func prettyName(name string) string {
	base := strings.TrimSuffix(name, ".wav")
	parts := strings.FieldsFunc(base, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	if len(parts) == 0 {
		return base
	}
	return strings.Join(parts, " ")
}

func playSound(clip soundClip) {
	if len(clip.data) == 0 {
		go defaultBell()
		return
	}

	go func(c soundClip) {
		ctx, err := ensureAudioContext()
		if err != nil {
			defaultBell()
			return
		}

		player := ctx.NewPlayer(bytes.NewReader(c.data))
		player.Play()

		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if !player.IsPlaying() {
				break
			}
		}
		_ = player.Close()
	}(clip)
}

const (
	audioSampleRate = 44100
	audioChannels   = 1
)

var (
	audioCtxOnce sync.Once
	audioCtx     *oto.Context
	audioCtxErr  error
	audioReady   chan struct{}
	readyOnce    sync.Once
)

func ensureAudioContext() (*oto.Context, error) {
	audioCtxOnce.Do(func() {
		ctx, ready, err := oto.NewContext(audioSampleRate, audioChannels, oto.FormatSignedInt16LE)
		if err != nil {
			audioCtxErr = err
			return
		}
		audioCtx = ctx
		audioReady = ready
	})

	if audioCtxErr != nil {
		return nil, audioCtxErr
	}

	readyOnce.Do(func() {
		if audioReady != nil {
			<-audioReady
		}
	})

	return audioCtx, nil
}

func defaultBell() {
	fmt.Print("\a")
}

func notify(title, body string) {
	_ = beeep.Notify(title, body, "")
}

func main() {
	if err := tea.NewProgram(initialModel()).Start(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
