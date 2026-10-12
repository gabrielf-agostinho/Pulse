package tui

import "github.com/charmbracelet/bubbles/key"

type globalKeys struct {
	Screens  key.Binding
	Next     key.Binding
	Prev     key.Binding
	Focus    key.Binding
	Help     key.Binding
	Refresh  key.Binding
	Pause    key.Binding
	Quit     key.Binding
	Back     key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Enter    key.Binding
}

var keys = globalKeys{
	Screens:  key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6"), key.WithHelp("1-6", "screens")),
	Next:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next screen")),
	Prev:     key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev screen")),
	Focus:    key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "focus")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Pause:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
	Top:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	Bottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "details")),
}

var (
	kFilter    = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter"))
	kSort      = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort"))
	kReverse   = key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "reverse"))
	kSortCPU   = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "by cpu"))
	kSortMem   = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "by memory"))
	kKill      = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "terminate"))
	kAck       = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "acknowledge"))
	kSeverity  = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "severity"))
	kRangeNext = key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("←/→", "range"))
	kRangePrev = key.NewBinding(key.WithKeys("left", "h"))
	kConfirm   = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm"))
	kCancel    = key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "cancel"))
)

type helpGroup struct {
	title    string
	bindings []key.Binding
}

func globalHelp() helpGroup {
	return helpGroup{"Global", []key.Binding{
		keys.Screens, keys.Prev, keys.Next, keys.Focus, keys.Up, keys.Down, keys.PageDown, keys.Top,
		keys.Enter, keys.Back, keys.Refresh, keys.Pause, keys.Help, keys.Quit,
	}}
}
