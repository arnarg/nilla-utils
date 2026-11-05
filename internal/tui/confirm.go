package tui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

type confirmModel struct {
	message string
	done    bool
	answer  bool
}

func (m confirmModel) Init() tea.Cmd {
	return nil
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y":
			m.done = true
			m.answer = true
		default:
			m.done = true
			m.answer = false
		}
		return m, tea.Quit
	}

	return m, nil
}

func (m confirmModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}

	return tea.NewView(fmt.Sprintf("\n%s [y/n]", m.message))
}

// RunTypedConfirm asks the user to type expected, for confirming destructive
// actions where a single keypress is too easy.
func RunTypedConfirm(msg, expected string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("confirmation requires a terminal, pass --confirm to skip it")
	}

	fmt.Fprintf(os.Stderr, "\n%s ", msg)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	return strings.TrimSpace(line) == expected, nil
}

func RunConfirm(msg string) (bool, error) {
	// Initialize model
	init := confirmModel{
		message: msg,
	}

	// Create a bubbletea program
	p := tea.NewProgram(
		init,
		tea.WithOutput(os.Stderr),
	)

	// Run program
	m, err := p.Run()
	if err != nil {
		return false, err
	}

	model := m.(confirmModel)

	return model.answer, nil
}
