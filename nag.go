// go mod init nag && go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/gen2brain/beeep
package main

import (
  "fmt"
  "os"
  "os/exec"
  "time"

  tea "github.com/charmbracelet/bubbletea"
  "github.com/charmbracelet/lipgloss"
  "github.com/gen2brain/beeep"
)

type tickMsg time.Time

type model struct {
  remaining time.Duration
  running   bool
  lastBeep  time.Time
  interval  time.Duration // nag repeat (e.g. 10s for demo; set to 1m/2m)
}

func initialModel() model {
  return model{remaining: 10 * time.Minute, running: false, interval: 0}
}

func (m model) Init() tea.Cmd {
  return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
  switch msg := msg.(type) {
  case tickMsg:
    if m.running && m.remaining > 0 {
      m.remaining -= time.Second
      if m.remaining == 0 {
        notify("Nag", "Time's up")
        play()
        m.lastBeep = time.Time{}
      } else if m.remaining < 0 {
        // re-nag at interval if set
        if m.interval > 0 && (m.lastBeep.IsZero() || time.Since(m.lastBeep) >= m.interval) {
          play()
          m.lastBeep = time.Now()
        }
      }
    }
    return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })

  case tea.KeyMsg:
    switch msg.String() {
    case "q", "ctrl+c":
      return m, tea.Quit
    case " ":
      m.running = !m.running
    case "s":
      m.running = false
      m.remaining = 0
    case "+":
      m.remaining += time.Minute
    case "2":
      m.remaining += 2 * time.Minute
    case "5":
      m.remaining += 5 * time.Minute
    case "0":
      m.remaining += 10 * time.Minute
    case "h":
      m.remaining += time.Hour
    case "r":
      m.remaining = 10 * time.Minute
      m.running = true
		case "up":
			m.remaining += time.Minute
		case "down":
			if m.remaining > time.Minute {
				m.remaining -= time.Minute
			} else {
				m.remaining = 0
			}
		}
  }
  return m, nil
}

func (m model) View() string {
  title := lipgloss.NewStyle().Bold(true).Render("Nag (TUI)")
  mins := int(m.remaining / time.Minute)
  secs := int(m.remaining%time.Minute) / int(time.Second)
  status := "paused"
  if m.running { status = "running" }
  help := "keys: space start/pause • s stop • 2/+2m, 5/+5m, 0/+10m, + +1m, h +1h • r reset • q quit"
  return fmt.Sprintf("%s\n\n  %02d:%02d  (%s)\n\n  %s\n",
    title, mins, secs, status, help)
}

func notify(title, body string) {
  _ = beeep.Notify(title, body, "")
}

func play() {
  // Try paplay/aplay; fall back to terminal bell
  if _, err := exec.LookPath("paplay"); err == nil {
    _ = exec.Command("paplay", "/usr/share/sounds/freedesktop/stereo/complete.oga").Start()
    return
  }
  if _, err := exec.LookPath("aplay"); err == nil {
    _ = exec.Command("aplay", "/usr/share/sounds/alsa/Front_Center.wav").Start()
    return
  }
  fmt.Print("\a")
}

func main() {
  if err := tea.NewProgram(initialModel()).Start(); err != nil {
    fmt.Println("error:", err)
    os.Exit(1)
  }
}

