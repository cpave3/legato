package hooks

// PathGrantFlags grants a Chimera session files and folders outside its
// working directory: one --add-read-path or --add-write-path for each path,
// quoted for the shell, so a path with spaces is one argument.
func (a *ChimeraAdapter) PathGrantFlags(read, write []string) string {
	flags := ""
	for _, path := range read {
		flags += " --add-read-path " + shellQuote(path)
	}
	for _, path := range write {
		flags += " --add-write-path " + shellQuote(path)
	}
	return flags
}
