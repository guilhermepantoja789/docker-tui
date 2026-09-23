package styles

import "github.com/charmbracelet/lipgloss"

var (
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))

	Header = lipgloss.NewStyle().
		Foreground(lipgloss.Color("250")).
		Bold(true)

	Muted = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	StatusOK = lipgloss.NewStyle().
		Foreground(lipgloss.Color("82"))

	StatusWarn = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	StatusErr = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196"))

	TabActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Padding(0, 1)

	TabInactive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Padding(0, 1)

	Selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("236")).
		Bold(true)

	Help = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	Modal = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2)

	Running = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	Exited  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	Paused  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Other   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// StateStyle returns a style for a container state string.
func StateStyle(state string) lipgloss.Style {
	switch state {
	case "running":
		return Running
	case "exited", "dead":
		return Exited
	case "paused":
		return Paused
	default:
		return Other
	}
}
