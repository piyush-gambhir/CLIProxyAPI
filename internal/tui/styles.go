package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary   = lipgloss.Color("#7C3AED")
	colorSecondary = lipgloss.Color("#6366F1")
	colorSuccess   = lipgloss.Color("#22C55E")
	colorWarning   = lipgloss.Color("#EAB308")
	colorError     = lipgloss.Color("#EF4444")
	colorInfo      = lipgloss.Color("#3B82F6")
	colorMuted     = lipgloss.Color("#6B7280")
	colorBg        = lipgloss.Color("#1E1E2E")
	colorSurface   = lipgloss.Color("#313244")
	colorText      = lipgloss.Color("#CDD6F4")
	colorSubtext   = lipgloss.Color("#A6ADC8")
	colorBorder    = lipgloss.Color("#45475A")
	colorHighlight = lipgloss.Color("#F5C2E7")
)

var (
	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(colorPrimary).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorSubtext).
				Background(colorSurface).
				Padding(0, 2)

	tabBarStyle = lipgloss.NewStyle().
			Background(colorSurface).
			PaddingLeft(1).
			PaddingBottom(0)
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorHighlight).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Italic(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorInfo).
			Bold(true).
			Width(24)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	sectionStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Background(colorSurface).
			PaddingLeft(1).
			PaddingRight(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)

var (
	logDebugStyle = lipgloss.NewStyle().Foreground(colorMuted)
	logInfoStyle  = lipgloss.NewStyle().Foreground(colorInfo)
	logWarnStyle  = lipgloss.NewStyle().Foreground(colorWarning)
	logErrorStyle = lipgloss.NewStyle().Foreground(colorError)
)

var (
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorHighlight).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorBorder)

	tableCellStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingRight(2)

	tableSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(colorPrimary).
				Bold(true)
)

func logLevelStyle(level string) lipgloss.Style {
	switch level {
	case "debug":
		return logDebugStyle
	case "info":
		return logInfoStyle
	case "warn", "warning":
		return logWarnStyle
	case "error", "fatal", "panic":
		return logErrorStyle
	default:
		return logInfoStyle
	}
}
