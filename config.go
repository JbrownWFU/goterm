package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Opener is an external program offered by the "open with" picker (E).
type Opener struct {
	Name string   `json:"name"`
	Cmd  string   `json:"cmd"`
	Args []string `json:"args,omitempty"`
}

type Config struct {
	// Programs offered by E. Ones that aren't installed are hidden.
	Openers []Opener `json:"openers"`
	// Content for new .md files. {{title}} and {{date}} are expanded.
	NewFileTemplate string `json:"newFileTemplate"`
	// Command run in a new Windows Terminal tab by "c".
	ClaudeCommand []string `json:"claudeCommand"`
	// Command run in a new Windows Terminal tab by "s" (empty = default profile).
	ShellCommand []string `json:"shellCommand"`
	// Glamour style for markdown preview: tokyo-night, dark, light, dracula, pink, ascii, notty.
	GlamourStyle string `json:"glamourStyle"`
	// Show dotfiles on startup.
	ShowHidden bool `json:"showHidden"`
	// Folder names skipped by find (f), matched case-insensitively.
	FindSkip []string `json:"findSkip"`
	// Folder listing order, managed with S.
	Sort SortSpec `json:"sort"`
	// Saved locations, managed with b / B.
	Bookmarks []Bookmark `json:"bookmarks"`
}

// Bookmark is a saved folder to jump to.
type Bookmark struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func defaultConfig() Config {
	return Config{
		Openers: []Opener{
			{Name: "Notepad++", Cmd: `C:\Program Files\Notepad++\notepad++.exe`},
			{Name: "Notepad++ (x86)", Cmd: `C:\Program Files (x86)\Notepad++\notepad++.exe`},
			{Name: "VS Code", Cmd: "code"},
			{Name: "Notepad", Cmd: "notepad"},
			{Name: "Default app", Cmd: "explorer"},
		},
		NewFileTemplate: "# {{title}}\n\n_{{date}}_\n\n",
		ClaudeCommand:   []string{"powershell", "-NoExit", "-Command", "claude"},
		ShellCommand:    nil,
		GlamourStyle:    "tokyo-night",
		Sort:            SortSpec{By: "name", DirsFirst: true},
		FindSkip:        []string{"node_modules", ".git", "__pycache__", ".venv", "venv", ".tox", ".mypy_cache", ".pytest_cache"},
	}
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "goterm", "config.json")
}

// loadConfig reads the user's config, writing the defaults on first run so
// there's a file to edit. firstRun reports that the file was just created.
func loadConfig() (cfg Config, firstRun bool) {
	cfg = defaultConfig()
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
				_ = os.MkdirAll(filepath.Dir(path), 0o755)
				_ = os.WriteFile(path, b, 0o644)
				firstRun = true
			}
		}
		return cfg, firstRun
	}
	_ = json.Unmarshal(data, &cfg)
	if !cfg.Sort.valid() {
		cfg.Sort.By = "name"
	}
	if cfg.GlamourStyle == "" {
		cfg.GlamourStyle = "tokyo-night"
	}
	return cfg, false
}

func saveBookmarks(path string, bms []Bookmark) error {
	return updateConfigFile(path, func(c *Config) { c.Bookmarks = bms })
}

// updateConfigFile applies change to the config file at path, re-reading the
// file first so edits made while goterm was running aren't lost.
func updateConfigFile(path string, change func(*Config)) error {
	cfg := defaultConfig()
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("config.json is not valid JSON, change not saved: %w", err)
		}
	}
	change(&cfg)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (o Opener) available() bool {
	if filepath.IsAbs(o.Cmd) {
		_, err := os.Stat(o.Cmd)
		return err == nil
	}
	_, err := exec.LookPath(o.Cmd)
	return err == nil
}

func availableOpeners(all []Opener) []Opener {
	var out []Opener
	for _, o := range all {
		if o.available() {
			out = append(out, o)
		}
	}
	return out
}

func expandTemplate(tpl, filename string) string {
	title := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	r := strings.NewReplacer(
		"{{title}}", title,
		"{{date}}", time.Now().Format("2006-01-02"),
	)
	return r.Replace(tpl)
}
