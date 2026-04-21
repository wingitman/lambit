package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Keybinds holds all configurable key mappings.
type Keybinds struct {
	Up        string `toml:"up"`
	Down      string `toml:"down"`
	Confirm   string `toml:"confirm"`
	Back      string `toml:"back"`
	Options   string `toml:"options"`
	Quit      string `toml:"quit"`
	Invoke    string `toml:"invoke"`
	NewTest   string `toml:"new_test"`
	Edit      string `toml:"edit"`
	Delete    string `toml:"delete"`
	ToggleAPI string `toml:"toggle_api"`
	Benchmark string `toml:"benchmark"`
	Scaffold  string `toml:"scaffold"`
	Help      string `toml:"help"`
	PageUp    string `toml:"page_up"`
	PageDown  string `toml:"page_down"`
}

// Apps holds default application overrides.
type Apps struct {
	Editor string `toml:"editor"`
}

// Config is the root config struct.
type Config struct {
	Keybinds Keybinds `toml:"keybinds"`
	Apps     Apps     `toml:"apps"`
}

// keybindEntries is the authoritative list of every keybind TOML key name
// paired with its comment. Adding a new keybind here propagates it through
// migration and default-filling automatically.
var keybindEntries = []struct{ key, comment string }{
	{"up", "move cursor up"},
	{"down", "move cursor down"},
	{"confirm", "confirm / select"},
	{"back", "go back / cancel"},
	{"quit", "quit lambit"},
	{"options", "open config file in $EDITOR"},
	{"invoke", "invoke selected function / test"},
	{"new_test", "create a new test case"},
	{"edit", "edit selected item (handler / payload / model)"},
	{"delete", "delete selected test case or model"},
	{"toggle_api", "start / stop local HTTP API server"},
	{"benchmark", "toggle benchmark pane"},
	{"scaffold", "scaffold .lambit.toml in current project"},
	{"help", "show keybind help overlay"},
	{"page_up", "page up"},
	{"page_down", "page down"},
}

var appEntries = []string{"editor"}

// Default returns a Config with all default values.
func Default() *Config {
	return &Config{
		Keybinds: Keybinds{
			Up:        "up",
			Down:      "down",
			Confirm:   "enter",
			Back:      "esc",
			Options:   "o",
			Quit:      "q",
			Invoke:    "i",
			NewTest:   "n",
			Edit:      "e",
			Delete:    "d",
			ToggleAPI: "a",
			Benchmark: "b",
			Scaffold:  "s",
			Help:      "?",
			PageUp:    "pgup",
			PageDown:  "pgdown",
		},
		Apps: Apps{
			Editor: "",
		},
	}
}

// ConfigDir returns the platform-appropriate config directory.
//   - Windows: %APPDATA%\delbysoft
//   - macOS:   ~/Library/Application Support/delbysoft
//   - Linux:   ~/.config/delbysoft
func ConfigDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return ""
		}
		return filepath.Join(home, ".config", "delbysoft")
	}
	return filepath.Join(base, "delbysoft")
}

// ConfigPath returns the full path to the lambit config file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "lambit.toml")
}

// Load reads the config file, creating it with defaults if it doesn't exist.
func Load() (*Config, error) {
	cfg := Default()
	path := ConfigPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
			return cfg, nil
		}
		if err := WriteDefault(path); err != nil {
			return cfg, nil
		}
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return Default(), err
	}

	applyKeybindDefaults(cfg)

	if needsMigration(path) {
		_ = writeMigrated(path, cfg)
	}

	return cfg, nil
}

func applyKeybindDefaults(cfg *Config) {
	d := Default().Keybinds
	if cfg.Keybinds.Up == ""        { cfg.Keybinds.Up = d.Up }
	if cfg.Keybinds.Down == ""      { cfg.Keybinds.Down = d.Down }
	if cfg.Keybinds.Confirm == ""   { cfg.Keybinds.Confirm = d.Confirm }
	if cfg.Keybinds.Back == ""      { cfg.Keybinds.Back = d.Back }
	if cfg.Keybinds.Options == ""   { cfg.Keybinds.Options = d.Options }
	if cfg.Keybinds.Quit == ""      { cfg.Keybinds.Quit = d.Quit }
	if cfg.Keybinds.Invoke == ""    { cfg.Keybinds.Invoke = d.Invoke }
	if cfg.Keybinds.NewTest == ""   { cfg.Keybinds.NewTest = d.NewTest }
	if cfg.Keybinds.Edit == ""      { cfg.Keybinds.Edit = d.Edit }
	if cfg.Keybinds.Delete == ""    { cfg.Keybinds.Delete = d.Delete }
	if cfg.Keybinds.ToggleAPI == "" { cfg.Keybinds.ToggleAPI = d.ToggleAPI }
	if cfg.Keybinds.Benchmark == "" { cfg.Keybinds.Benchmark = d.Benchmark }
	if cfg.Keybinds.Scaffold == ""  { cfg.Keybinds.Scaffold = d.Scaffold }
	if cfg.Keybinds.Help == ""      { cfg.Keybinds.Help = d.Help }
	if cfg.Keybinds.PageUp == ""    { cfg.Keybinds.PageUp = d.PageUp }
	if cfg.Keybinds.PageDown == ""  { cfg.Keybinds.PageDown = d.PageDown }
}

func needsMigration(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(data)
	for _, e := range keybindEntries {
		if !fileContainsKey(content, e.key) {
			return true
		}
	}
	for _, key := range appEntries {
		if !fileContainsKey(content, key) {
			return true
		}
	}
	return false
}

func fileContainsKey(content, key string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+"=") || strings.HasPrefix(trimmed, key+" ") {
			return true
		}
	}
	return false
}

func writeMigrated(path string, cfg *Config) error {
	return os.WriteFile(path, []byte(buildTOML(cfg)), 0644)
}

// WriteDefault writes the default config file to path.
func WriteDefault(path string) error {
	return os.WriteFile(path, []byte(buildTOML(Default())), 0644)
}

func buildTOML(cfg *Config) string {
	vals := keybindValues(&cfg.Keybinds)
	maxLen := 0
	for _, e := range keybindEntries {
		if len(e.key) > maxLen {
			maxLen = len(e.key)
		}
	}
	out := "# lambit configuration file\n" +
		"# Key values: use BubbleTea names like \"up\", \"down\", \"enter\", \"esc\",\n" +
		"# or single characters like \"q\", \"i\", \"n\".\n\n" +
		"[keybinds]\n"
	for _, e := range keybindEntries {
		val := vals[e.key]
		pad := strings.Repeat(" ", maxLen-len(e.key))
		out += e.key + pad + " = " + quote(val) + "  # " + e.comment + "\n"
	}
	out += "\n[apps]\n" +
		"editor = " + quote(cfg.Apps.Editor) + "   # leave empty to use $EDITOR env var\n"
	return out
}

func keybindValues(k *Keybinds) map[string]string {
	return map[string]string{
		"up":         k.Up,
		"down":       k.Down,
		"confirm":    k.Confirm,
		"back":       k.Back,
		"quit":       k.Quit,
		"options":    k.Options,
		"invoke":     k.Invoke,
		"new_test":   k.NewTest,
		"edit":       k.Edit,
		"delete":     k.Delete,
		"toggle_api": k.ToggleAPI,
		"benchmark":  k.Benchmark,
		"scaffold":   k.Scaffold,
		"help":       k.Help,
		"page_up":    k.PageUp,
		"page_down":  k.PageDown,
	}
}

func quote(s string) string { return `"` + s + `"` }
