package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type helpNode struct {
	path, summary, usage, arguments, environment, examples string
	hidden                                                 bool
	children                                               []helpNode
}

func helpTree() helpNode {
	const auditExample = "chezmoi dots audit aliases"
	chezEnv := "CHEZMOI_EXECUTABLE  chezmoi executable (default: chezmoi).\nCHEZMOI_CONFIG_FILE  chezmoi config file (default: chezmoi's config).\nCHEZMOI_SOURCE_DIR  source directory (default: ask chezmoi; honors .chezmoiroot)."
	return helpNode{
		summary:     "Manage, verify and render dotfiles",
		usage:       "chezmoi dots [command] [arguments] [options]",
		arguments:   "command  Default: edit. With no selector, open the interactive picker.\nAn unrecognized first token is an edit selector, not a command.",
		environment: "See command help for relevant environment variables.",
		examples:    "chezmoi dots\nchezmoi dots help view\nchezmoi dots view --help",
		children: []helpNode{
			{path: cmdEdit, summary: "Edit dotfiles source (default command)", usage: "chezmoi dots [edit] [<what>]", arguments: "what  Registry entry, source path or target path (default: interactive picker).", environment: chezEnv + "\nEDITOR / VISUAL  editor selection: EDITOR, then VISUAL, then vi (notepad on Windows).\nTMUX / WT_SESSION  enable the live preview pane when the terminal supports it.", examples: "chezmoi dots\nchezmoi dots edit shell\nchezmoi dots dot_config/git/config.tmpl"},
			{path: cmdView, summary: "Preview rendered dotfiles", usage: "chezmoi dots view [<what>] [options]", arguments: "what  Registry entry, source path or target path. Required unless --matrix or --all requests an overview.\nDefault: render the current configuration; --matrix/--all render all nine OS/role configurations.", environment: chezEnv, examples: "chezmoi dots view shell\nchezmoi dots view shell --as linux,work\nchezmoi dots view --matrix"},
			{path: cmdList, summary: "List configured editable registry components", usage: "chezmoi dots list", arguments: "None. Lists all configured entries; no pattern argument is supported.", environment: chezEnv, examples: "chezmoi dots list"},
			{path: cmdLint, summary: "Check registries and rendered templates", usage: "chezmoi dots lint [<check>...]", arguments: "check  registry, aliases, packages, matrix, templates (default: all checks).\nChecks are arguments, not subcommands; supplied order and repetitions are preserved.", environment: chezEnv, examples: "chezmoi dots lint\nchezmoi dots lint registry templates"},
			{path: cmdAudit, summary: "Audit live shell configuration", usage: auditExample, arguments: "aliases  Required audit command; there is no default audit.", environment: "See aliases help for shell configuration locations.", examples: "chezmoi dots audit aliases\nchezmoi dots help audit aliases", children: []helpNode{
				{path: "audit aliases", summary: "Detect unmanaged live shell aliases", usage: auditExample, arguments: "None. Checks available zsh and bash interactive shells.", environment: "XDG_CONFIG_HOME  config directory (default: ~/.config).\nReads shell/aliases.sh and shell/omz-aliases.zsh there, and ~/.oh-my-zsh/plugins.", examples: auditExample},
			}},
			{path: cmdOutdated, summary: "Every tool: installed, pinned and latest versions", usage: "chezmoi dots outdated [options]", arguments: "None. Default: inspect installed tools and look up latest versions without fetching git.", environment: chezEnv + "\nCHEZMOI_WORKING_TREE  dotfiles working tree (default: ask chezmoi).", examples: "chezmoi dots outdated\nchezmoi dots outdated --offline\nchezmoi dots outdated --fetch --check"},
			{path: cmdBump, summary: "Update pinned versions and checksums", usage: "chezmoi dots bump [<release>...] [options]", arguments: "release  Name from the release registry (default: all releases).\nDefault: write updated pins and checksums to .chezmoidata/packages.toml.", environment: chezEnv, examples: "chezmoi dots bump --dry-run\nchezmoi dots bump example-tool"},
			{path: cmdUpgrade, summary: "Run system and dotfile upgrade steps", usage: "chezmoi dots upgrade [<step>...] [options]", arguments: "step  dotfiles, brew, apt, winget, uv, npm, psmodules (OS-specific).\nDefault: all steps for this OS, then check pinned releases. Steps are arguments, not subcommands; execution follows the built-in plan order.\nDefault dry run still checks pinned releases and can use the network.", environment: chezEnv + "\nPATH  determines available package managers.", examples: "chezmoi dots upgrade --dry-run\nchezmoi dots upgrade dotfiles uv"},
			{path: cmdVersion, summary: "Print version information", usage: "chezmoi dots version", arguments: "None required. Extra positional arguments are ignored.", environment: "None.", examples: "chezmoi dots version"},
			{path: cmdHelp, summary: "Show command help", usage: "chezmoi dots help [<command> [<subcommand>]]", arguments: "command  Help page (default: root index). Only audit has a subcommand: aliases.\nInternal live help is available explicitly but omitted from the root index.", environment: "None.", examples: "chezmoi dots help\nchezmoi dots help view\nchezmoi dots help audit aliases\nchezmoi dots help live"},
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
	renderHelp(d.Stdout, node)
	return nil
}

func renderHelp(w io.Writer, node helpNode) {
	name := strings.TrimSpace("chezmoi dots " + node.path)
	fmt.Fprintf(w, "%s — %s\n\nUSAGE\n  %s\n", name, node.summary, node.usage)
	if len(node.children) > 0 {
		fmt.Fprintln(w, "\nCOMMANDS")
		for _, child := range node.children {
			if !child.hidden {
				fmt.Fprintf(w, "  %-10s %s\n", strings.TrimPrefix(child.path, node.path+" "), child.summary)
			}
		}
	}
	helpSection(w, "ARGUMENTS", node.arguments)
	fmt.Fprintln(w, "\nOPTIONS\n  -h, --help  Show this help and exit successfully.")
	fs := newFlags(node.path)
	fs.VisitAll(func(f *flag.Flag) {
		value, description := flag.UnquoteUsage(f)
		option := "--" + f.Name
		if value != "" {
			option += " <" + value + ">"
		}
		fmt.Fprintf(w, "  %s  %s (default: %q)\n", option, description, f.DefValue)
	})
	helpSection(w, "ENVIRONMENT", node.environment)
	helpSection(w, "EXAMPLES", node.examples)
}

func helpSection(w io.Writer, heading, text string) {
	fmt.Fprintf(w, "\n%s\n", heading)
	for line := range strings.SplitSeq(text, "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}
