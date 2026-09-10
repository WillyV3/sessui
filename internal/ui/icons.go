package ui

import "github.com/charmbracelet/lipgloss"

// Icon is one app's glyph identity: the Nerd Font name the codepoint came
// from, the glyph itself, and the colour it renders in.
type Icon struct {
	Name  string
	Glyph string
	Color lipgloss.Color
}

// Glyphs are fixed; colours come from the active palette via applyTheme, so
// nothing here is a hardcoded hex. Assigned once at startup, hence package
// vars rather than values threaded through every render.
var (
	iconClaude, iconAgent, iconCodex, iconNvim, iconOpencode        Icon
	iconCopilot, iconGemini                                         Icon
	iconCrush, iconAider, iconEditor, iconHelix                     Icon
	iconGit, iconDocker, iconK8s, iconMonitor                       Icon
	iconDust, iconBandwhich, iconSearch, iconFiles, iconZoxide      Icon
	iconTelevision, iconStarship, iconFastfetch, iconJq, iconBat    Icon
	iconGo, iconRust, iconPython, iconNode                          Icon
	iconMpv, iconImv, iconCava, iconSpotify, iconMusic              Icon
	iconImpala, iconWiremix, iconBluetui                            Icon
	iconNeomutt, iconWeechat, iconNewsboat, iconCalcurse, iconAtuin Icon
	iconShell, iconOther                                            Icon
)

// applyTheme fills every icon's colour from the palette. claude is orange
// and other agents blue, which is how a row flags an agent at a glance; the
// rest take the tool's conventional slot. Call once, before the delegate is
// built.
func applyTheme(p Palette) {
	iconClaude = Icon{"nf-cod-claude", glyphU(0xEC82), p.Orange}
	iconAgent = Icon{"nf-md-robot", glyphU(0xF06A9), p.Blue}
	iconCodex = Icon{"nf-cod-openai", glyphU(0xEC81), p.Blue}
	iconNvim = Icon{"nf-custom-neovim", glyphU(0xE6AE), p.Green}
	iconOpencode = Icon{"nf-fa-code", glyphU(0xF121), p.Blue}
	iconCopilot = Icon{"nf-cod-copilot", glyphU(0xEC1E), p.Blue}
	iconGemini = Icon{"nf-dev-google", glyphU(0xE7F0), p.Blue}
	iconCrush = Icon{"nf-fa-gem", glyphU(0xF219), p.Blue}
	iconAider = Icon{"nf-robot_outline", glyphU(0xEC20), p.Blue}

	iconEditor = Icon{"nf-custom-vim", glyphU(0xE62B), p.Green}
	iconHelix = Icon{"nf-helix", glyphU(0xEAC4), p.Green}

	iconGit = Icon{"nf-custom-git", glyphU(0xEC6F), p.Red}
	iconDocker = Icon{"nf-dev-docker", glyphU(0xE7B0), p.Blue}
	iconK8s = Icon{"nf-md-kubernetes", glyphU(0xF10FE), p.Blue}

	iconMonitor = Icon{"nf-fa-tachometer", glyphU(0xF0E4), p.Magenta}
	iconDust = Icon{"nf-md-harddisk", glyphU(0xF02CA), p.Yellow}
	iconBandwhich = Icon{"nf-md-network", glyphU(0xF06F3), p.Cyan}

	iconSearch = Icon{"nf-md-magnify", glyphU(0xF0349), p.Accent}
	iconFiles = Icon{"nf-md-folder", glyphU(0xF024B), p.Blue}
	iconZoxide = Icon{"nf-fa-jump", glyphU(0xF14E), p.Cyan}
	iconTelevision = Icon{"nf-md-television", glyphU(0xF0502), p.Accent}

	iconStarship = Icon{"nf-fa-rocket", glyphU(0xF135), p.Accent}
	iconFastfetch = Icon{"nf-fastfetch", glyphU(0xF31A), p.Blue}
	iconJq = Icon{"nf-jq", glyphU(0xEB0F), p.Yellow}
	iconBat = Icon{"nf-md-bat", glyphU(0xF0B5F), p.Yellow}
	iconGo = Icon{"nf-dev-go", glyphU(0xE724), p.Cyan}
	iconRust = Icon{"nf-dev-rust", glyphU(0xE7A8), p.Orange}
	iconPython = Icon{"nf-dev-python", glyphU(0xE606), p.Yellow}
	iconNode = Icon{"nf-dev-nodejs_small", glyphU(0xE718), p.Green}

	iconMpv = Icon{"nf-md-mpv", glyphU(0xF040A), p.Magenta}
	iconImv = Icon{"nf-md-image", glyphU(0xF02E9), p.Magenta}
	iconCava = Icon{"nf-md-equalizer", glyphU(0xF0EA2), p.Magenta}
	iconSpotify = Icon{"nf-md-spotify", glyphU(0xF04C7), p.Green}
	iconMusic = Icon{"nf-fa-music", glyphU(0xF001), p.Magenta}

	iconImpala = Icon{"nf-md-impala", glyphU(0xF05A9), p.Blue}
	iconWiremix = Icon{"nf-md-wiremix", glyphU(0xF057E), p.Magenta}
	iconBluetui = Icon{"nf-md-bluetooth", glyphU(0xF00AF), p.Blue}

	iconNeomutt = Icon{"nf-md-email", glyphU(0xF01EE), p.Blue}
	iconWeechat = Icon{"nf-md-forum", glyphU(0xF028C), p.Green}
	iconNewsboat = Icon{"nf-md-rss", glyphU(0xF0395), p.Foreground}
	iconCalcurse = Icon{"nf-md-calendar", glyphU(0xF00ED), p.Foreground}
	iconAtuin = Icon{"nf-md-history", glyphU(0xF02DA), p.Foreground}

	iconShell = Icon{"nf-custom-shell", glyphU(0xEA85), p.Foreground}
	iconOther = Icon{"nf-fa-square_o", glyphU(0xF0C8), p.Foreground}
	appIcons = buildAppIcons()
}

// appIcons maps a running command to its Icon. Nil until applyTheme builds it.
var appIcons map[string]Icon

// buildAppIcons is built after applyTheme so the map captures themed values.
func buildAppIcons() map[string]Icon {
	return map[string]Icon{
		"claude": iconClaude,

		"opencode": iconOpencode, "copilot": iconCopilot, "gemini": iconGemini,
		"crush": iconCrush, "aider": iconAider,
		"codex": iconCodex, "cursor": iconAgent, "cline": iconAgent, "amp": iconAgent, "goose": iconAgent,

		"nvim": iconNvim, "vim": iconEditor, "vi": iconEditor,
		"helix": iconHelix, "hx": iconHelix,

		"git": iconGit, "gittui": iconGit, "lazygit": iconGit, "tig": iconGit, "gitui": iconGit,

		"docker": iconDocker, "lazydocker": iconDocker,
		"k9s": iconK8s, "kubectl": iconK8s,

		"btop": iconMonitor, "htop": iconMonitor, "top": iconMonitor, "bottom": iconMonitor, "btm": iconMonitor,
		"dust": iconDust, "dua": iconDust, "ncdu": iconDust, "gdu": iconDust,
		"bandwhich": iconBandwhich,

		"fzf": iconSearch, "ripgrep": iconSearch, "rg": iconSearch,
		"eza": iconFiles, "exa": iconFiles,
		"yazi": iconFiles, "ranger": iconFiles, "nnn": iconFiles, "lf": iconFiles, "superfile": iconFiles,
		"zoxide": iconZoxide, "z": iconZoxide,
		"television": iconTelevision, "tv": iconTelevision,

		"starship":  iconStarship,
		"fastfetch": iconFastfetch, "neofetch": iconFastfetch,
		"jq":  iconJq,
		"bat": iconBat,
		"go":  iconGo, "air": iconGo,
		"rust": iconRust, "cargo": iconRust,
		"python": iconPython, "python3": iconPython, "uv": iconPython, "ipython": iconPython,
		"node": iconNode, "npm": iconNode, "bun": iconNode, "deno": iconNode,

		"mpv":            iconMpv,
		"imv":            iconImv,
		"cava":           iconCava,
		"spotify_player": iconSpotify, "ncspot": iconSpotify, "spotify-player": iconSpotify,
		"cliamp": iconMusic,

		"impala":  iconImpala,
		"wiremix": iconWiremix,
		"bluetui": iconBluetui,

		"neomutt": iconNeomutt, "aerc": iconNeomutt, "mutt": iconNeomutt,
		"weechat": iconWeechat, "irssi": iconWeechat, "senpai": iconWeechat,
		"newsboat": iconNewsboat,
		"calcurse": iconCalcurse,
		"atuin":    iconAtuin,

		"bash": iconShell, "zsh": iconShell, "fish": iconShell, "sh": iconShell,
	}
}

// iconFor looks up an app's Icon, falling back to a generic glyph.
func iconFor(app string) Icon {
	if icon, ok := appIcons[app]; ok {
		return icon
	}
	return iconOther
}
