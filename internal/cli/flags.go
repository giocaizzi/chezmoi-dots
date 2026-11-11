package cli

import "flag"

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	switch name {
	case cmdView:
		fs.String("as", "", "render one `os[,role]` (darwin|linux|windows; personal|work|light)")
		fs.Bool("matrix", false, "render every configuration")
		fs.Bool("all", false, "overview across all registry entries")
	case cmdOutdated:
		fs.Bool("offline", false, "skip network lookups")
		fs.Bool("fetch", false, "git fetch the dotfiles first")
		fs.Bool("check", false, "exit non-zero for outdated, drifted or missing tools")
	case cmdBump:
		fs.Bool("dry-run", false, "show what would change, write nothing")
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

func boolValue(fs *flag.FlagSet, name string) bool {
	getter, _ := fs.Lookup(name).Value.(flag.Getter) // all callers name registered boolean flags
	value, _ := getter.Get().(bool)
	return value
}

func stringValue(fs *flag.FlagSet, name string) string {
	return fs.Lookup(name).Value.String()
}
