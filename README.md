# Nag.go

A dead-simple TUI app where you set a timer
that will nag you after it's completed.

## Building

### Get dependencies

go mod init nag && go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/gen2brain/beeep

### Build

go build -o nag

### Run

./nag
