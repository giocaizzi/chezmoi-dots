package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/giocaizzi/chezmoi-dots/internal/term"
)

type helpNode struct {
	path, summary, usage, arguments, environment, examples string
	hidden                                                 bool
	children                                               []helpNode
}

func helpTree() helpNode {
	chezEnv := "CHEZMOI_EXECUTABLE  chezmoi executable (default: chezmoi).\nCHEZMOI_CONFIG_FILE  chezmoi config file (default: chezmoi's config).\nCHEZMOI_SOURCE_DIR  source directory (default: ask chezmoi; honors .chezmoiroot)."
	return helpNode{
		summary:     "Manage, verify and render dotfiles",
		usage:       "chezmoi dots [command] [arguments] [options]",
		arguments:   "command  Default: edit. With no selector, open the interactive picker.\nAn unrecognized first token is an edit selector, not a command.",
		environment: "See command help for relevant environment variables.",
		examples:    "chezmoi dots\nchezmoi dots help view\nchezmoi dots view --help",
		children: []helpNode{
			{path: cmdEdit, summary: "Edit dotfiles source (default command)", usage: "chezmoi dots [edit] [<what>]", arguments: "what  Registry entry, source path or target path (default: interactive picker).", environment: chezEnv + "\nEDITOR / VISUAL  editor selection: EDITOR, then VISUAL, then vi (notepad on Windows).\nTMUX / WT_SESSION  enable the live preview pane when the terminal supports it.", examples: "chezmoi dots\nchezmoi dots edit shell\nchezmoi dots dot_config/git/config.tmpl"},
			{path: cmdAdd, summary: "Track a file and register it for editing", usage: "chezmoi dots add <path> [options]", arguments: "path  File to track (a target path; relative paths are from the current directory). With --local, the folder (or the file's folder) to register.\nA folder needs --local. For an unmanaged file the entry is written to .chezmoidata/edit.toml, then chezmoi add runs, so its git automation commits both; a managed file or a --local folder is only registered, following the same git settings as edit.", environment: chezEnv, examples: "chezmoi dots add ~/.config/example/config.toml --desc \"Example settings\"\nchezmoi dots add ~/.ssh/config --encrypt --name ssh\nchezmoi dots add ~/.config/example --local --desc \"Example local files\""},
			{path: cmdView, summary: "Preview rendered dotfiles", usage: "chezmoi dots view [<what>] [options]", arguments: "what  Registry entry, source path or target path. Required unless --matrix requests an overview.\nDefault: render the current configuration; --matrix renders every OS/profile configuration.", environment: chezEnv, examples: "chezmoi dots view shell\nchezmoi dots view shell --as linux,home\nchezmoi dots view --matrix"},
			{path: cmdList, summary: "List configured editable registry components", usage: "chezmoi dots list", arguments: "None. Lists all configured entries; no pattern argument is supported.", environment: chezEnv, examples: "chezmoi dots list"},
			{path: cmdStatus, summary: "Dotfiles repository and apply status", usage: "chezmoi dots status [options]", arguments: "None. Shows the dotfiles git checkout (branch, ahead/behind, local changes) and what chezmoi apply would change. No network access unless --fetch.", environment: chezEnv + "\nCHEZMOI_WORKING_TREE  dotfiles working tree (default: ask chezmoi).", examples: "chezmoi dots status\nchezmoi dots status --fetch --check"},
			{path: cmdUpgrade, summary: "Run system and dotfile upgrade steps", usage: "chezmoi dots upgrade [<step>...] [options]", arguments: "step  dotfiles, brew, apt, winget, uv, npm, psmodules (OS-specific).\nDefault: all steps for this OS. Steps are arguments, not subcommands; execution follows the built-in plan order.", environment: chezEnv + "\nPATH  determines available package managers.", examples: "chezmoi dots upgrade --dry-run\nchezmoi dots upgrade dotfiles uv"},
			{path: cmdLint, summary: "Check registries and rendered templates", usage: "chezmoi dots lint [<check>...]", arguments: "check  registry, aliases, packages, matrix, templates (default: all checks).\nChecks are arguments, not subcommands; supplied order and repetitions are preserved.", environment: chezEnv, examples: "chezmoi dots lint\nchezmoi dots lint registry templates"},
			{path: cmdAudit, summary: "Audit live shell configuration", usage: "chezmoi dots audit [<check>...]", arguments: "check  aliases: detect unmanaged live shell aliases in the available zsh and bash interactive shells (default: all checks).\nChecks are arguments, not subcommands.", environment: "XDG_CONFIG_HOME  config directory (default: ~/.config).\nReads shell/aliases.sh and shell/omz-aliases.zsh there, and ~/.oh-my-zsh/plugins.", examples: "chezmoi dots audit\nchezmoi dots audit aliases"},
			{path: cmdPkg, summary: "Package and pinned release lifecycle", usage: "chezmoi dots pkg <command> [options]", arguments: "command  status, bump or add; the command is the first argument after pkg.", environment: chezEnv, examples: "chezmoi dots pkg status\nchezmoi dots pkg bump --dry-run\nchezmoi dots pkg add example-tool --brew", children: []helpNode{
				{path: cmdPkgState, summary: "Every tool: installed, pinned and latest versions", usage: "chezmoi dots pkg status [options]", arguments: "None. Default: inspect installed tools and look up latest versions without fetching git.\nPinned archive externals are compared with their repository HEAD.\n--offline skips upstream lookups and the git fetch; brew and apt still report latest from their local index, and version commands or winget export may still reach the network.", environment: chezEnv + "\nCHEZMOI_WORKING_TREE  dotfiles working tree (default: ask chezmoi).", examples: "chezmoi dots pkg status\nchezmoi dots pkg status --offline\nchezmoi dots pkg status --fetch --check"},
				{path: cmdPkgBump, summary: "Update pinned versions and checksums in the source", usage: "chezmoi dots pkg bump [<pin>...] [options]", arguments: "pin  Release name or archive repository path (owner/repo) (default: all pins).\nDefault: write updated pins and checksums to .chezmoidata/packages.toml and the externals files.", environment: chezEnv, examples: "chezmoi dots pkg bump --dry-run\nchezmoi dots pkg bump example-tool\nchezmoi dots pkg bump octocat/example"},
				{path: cmdPkgAdd, summary: "Add a package or pinned release to the source", usage: "chezmoi dots pkg add <name> [--brew[=<formula>]] [--cask <name>] [--apt[=<package>]] [--winget <id>] [--uv[=<spec>]] [--npm[=<spec>]] [options]\n  chezmoi dots pkg add <name> --release --version <v> --url <template> --target <os/arch>=<name>... [options]\n  chezmoi dots pkg add <name> --github <owner/repo> [options]", arguments: "name  Entry name, letters, digits, - and _.\nPackage mode (default) takes the install sources from flags: a bare --brew, --apt, --uv or --npm installs <name>, --brew=<formula> installs that instead.\nWithout a source flag the package managers on this machine are asked: brew and cask on macOS, apt on Linux, winget on Windows, uv and npm everywhere. Only this machine is looked at, never a public index; the keys of other OSes come only from flags or from running pkg add there. Detected sources are confirmed one by one on a terminal, else need --yes. Any source flag turns detection off.\nAn existing package gets the keys it lacks; keys it has are never changed.\nRelease mode (--release) needs --version, --url (with {version}) and one or more --target; it downloads each asset to record its sha256 (--dry-run does not).\n--github reads the latest release of owner/repo (GH_TOKEN or GITHUB_TOKEN raise the rate limit) and proposes the entry from its asset names: darwin, linux and windows on amd64 and arm64 are recognized, checksums, signatures and installers are left out, archives win over bare binaries, a tie is reported as ambiguous. The proposal needs a yes on a terminal, else --yes; --dry-run still reads GitHub but downloads no archive.\nThe entry is added to .chezmoidata/packages.toml, which is validated afterwards, then staged, committed and pushed alone as far as the chezmoi git settings ask. An existing release is never changed.", environment: chezEnv + "\nGH_TOKEN / GITHUB_TOKEN  optional token for --github.", examples: "chezmoi dots pkg add wget --brew --apt\nchezmoi dots pkg add wget\nchezmoi dots pkg add example-app --cask example-app --roles personal\nchezmoi dots pkg add example-tool --release --version 1.2.3 --url https://example.com/{version}/tool-{target}.tar.gz --target linux/amd64=linux-x86_64\nchezmoi dots pkg add example-tool --github octocat/example-tool --dry-run"},
			}},
			{path: cmdVersion, summary: "Print version information", usage: "chezmoi dots version", arguments: "None required. Extra positional arguments are ignored.", environment: "None.", examples: "chezmoi dots version"},
			{path: cmdHelp, summary: "Show command help", usage: "chezmoi dots help [<command> [<subcommand>]]", arguments: "command  Help page (default: root index). Only pkg has subcommands: status, bump, add.\nInternal live help is available explicitly but omitted from the root index.", environment: "None.", examples: "chezmoi dots help\nchezmoi dots help view\nchezmoi dots help pkg status\nchezmoi dots help live"},
			{path: cmdLive, summary: "Internal live preview pane", usage: "chezmoi dots live [options]", arguments: "Internal command started by the edit preview pane. No positional arguments are required; extras are ignored.\nDefault: refresh the preview every 500ms.", environment: "None; pane configuration is passed through options.", examples: "chezmoi dots help live", hidden: true},
		},
	}
}

func helpPage(path string) helpNode {
	node := helpTree()
	for part := range strings.FieldsSeq(path) {
		for _, child := range node.children {
			if child.path == strings.TrimSpace(node.path+" "+part) {
				node = child
				break
			}
		}
	}
	return node
}

func (d Deps) help(args []string) error {
	pos, err := parse(newFlags("help"), args)
	if err != nil {
		return err
	}
	node := helpTree()
	for _, part := range pos {
		found := false
		for _, child := range node.children {
			if child.path == strings.TrimSpace(node.path+" "+part) {
				node, found = child, true
				break
			}
		}
		if !found {
			return &commandError{command: node.path, err: fmt.Errorf("%w: unknown help topic %q", ErrUsage, part)}
		}
	}
	renderHelp(d.Stdout, node, term.NewStyle(d.Stdout, d.Getenv))
	return nil
}

func renderHelp(w io.Writer, node helpNode, style term.Style) {
	name := strings.TrimSpace("chezmoi dots " + node.path)
	fmt.Fprintf(w, "%s%s%s — %s\n\n%sUSAGE%s\n  %s\n", style.B, name, style.O, node.summary, style.B, style.O, node.usage)
	if len(node.children) > 0 {
		fmt.Fprintf(w, "\n%sCOMMANDS%s\n", style.B, style.O)
		for _, child := range node.children {
			if !child.hidden {
				fmt.Fprintf(w, "  %s%-10s%s %s\n", style.M, strings.TrimPrefix(child.path, node.path+" "), style.O, child.summary)
			}
		}
	}
	helpSection(w, "ARGUMENTS", node.arguments, style)
	fmt.Fprintf(w, "\n%sOPTIONS%s\n  -h, --help  Show this help and exit successfully.\n", style.B, style.O)
	fs := newFlags(node.path)
	fs.VisitAll(func(f *flag.Flag) {
		value, description := flag.UnquoteUsage(f)
		option := "--" + f.Name
		if value != "" {
			option += " <" + value + ">"
		}
		fmt.Fprintf(w, "  %s  %s (default: %q)\n", option, description, f.DefValue)
	})
	helpSection(w, "ENVIRONMENT", node.environment, style)
	helpSection(w, "EXAMPLES", node.examples, style)
}

func helpSection(w io.Writer, heading, text string, style term.Style) {
	fmt.Fprintf(w, "\n%s%s%s\n", style.B, heading, style.O)
	for line := range strings.SplitSeq(text, "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}
