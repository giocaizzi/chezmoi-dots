package cli

import (
	"errors"
	"flag"
)

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	switch name {
	case cmdAdd:
		fs.String("name", "", "registry entry `name` (default: inferred from the path)")
		fs.String("desc", "", "entry `description` (default: prompt on a terminal)")
		fs.Bool("template", false, "add an unmanaged file as a template (chezmoi add --template)")
		fs.Bool("encrypt", false, "add an unmanaged file encrypted (chezmoi add --encrypt)")
		fs.Bool("local", false, "register the path's folder as a local entry, without chezmoi add")
	case cmdStatus:
		fs.Bool("fetch", false, "git fetch the dotfiles first")
		fs.Bool("check", false, "exit non-zero when the dotfiles are not in sync")
	case cmdView:
		fs.String("as", "", "render one `os[,role]` (darwin|linux|windows; personal|work|light)")
		fs.Bool("matrix", false, "render every configuration")
	case cmdPkgState:
		fs.Bool("offline", false, "skip upstream lookups (releases, uv, npm) and the git fetch")
		fs.Bool("fetch", false, "git fetch the dotfiles first")
		fs.Bool("check", false, "exit non-zero for outdated, drifted or missing tools")
	case cmdPkgBump:
		fs.Bool("dry-run", false, "show what would change, write nothing")
	case cmdPkgAdd:
		fs.Var(&optValue{}, "brew", "install with brew; --brew=<formula> when the formula differs from the name")
		fs.String("cask", "", "install with brew cask `name`")
		fs.Var(&optValue{}, "apt", "install with apt; --apt=<package> when the package differs from the name")
		fs.String("winget", "", "install with winget `id`")
		fs.Var(&optValue{}, "uv", "install with uv tool; --uv=<spec> when the spec differs from the name")
		fs.Var(&optValue{}, "npm", "install with npm; --npm=<spec> when the spec differs from the name")
		fs.String("roles", "", "limit to `roles` (comma list of personal, work, light)")
		fs.String("requires", "", "install only when this [data] `answer` is set")
		fs.Bool("release", false, "add a pinned release binary instead of a package")
		fs.String("version", "", "release `version` to pin")
		fs.String("url", "", "release download `template` with {version} (and {target})")
		fs.Var(new(stringList), "target", "repeatable release `os/arch=name` target")
		fs.Bool("dry-run", false, "print the entry, write nothing and download nothing")
	case cmdUpgrade:
		fs.Bool("dry-run", false, "show upgrade commands without executing upgrade steps")
	case cmdLive:
		fs.String("bin", "chezmoi", "chezmoi `executable`")
		fs.String("src", "", "shadow source `directory`")
		fs.String("real-src", "", "real source `directory`")
		fs.String("file", "", "source `file`")
		fs.String("edited", "", "edited `file`")
		fs.String("dir", "", "working `directory`")
		fs.Var(new(stringList), "global", "repeatable chezmoi global `argument`")
	}
	return fs
}

// optValue is a flag whose value is optional: bare, it stands for the package name;
// with =<value>, for that value.
type optValue struct {
	set bool
	val string
}

func (o *optValue) String() string {
	if o == nil {
		return ""
	}
	return o.val
}

func (o *optValue) Set(v string) error {
	if v == "" {
		return errors.New("needs a value after =")
	}
	o.set = true
	o.val = ""
	if v != "true" { // the flag package passes "true" for the bare form
		o.val = v
	}
	return nil
}

// IsBoolFlag lets the flag stand without a value, so a following word stays positional.
func (*optValue) IsBoolFlag() bool { return true }

// resolve is the flag's value, with name standing for a bare flag; empty when unset.
func (o *optValue) resolve(name string) string {
	switch {
	case o == nil || !o.set:
		return ""
	case o.val == "":
		return name
	}
	return o.val
}

func boolValue(fs *flag.FlagSet, name string) bool {
	getter, _ := fs.Lookup(name).Value.(flag.Getter) // all callers name registered boolean flags
	value, _ := getter.Get().(bool)
	return value
}

func stringValue(fs *flag.FlagSet, name string) string {
	return fs.Lookup(name).Value.String()
}
