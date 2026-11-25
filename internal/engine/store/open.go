package store

import "fmt"

type Options struct {
	Driver string
	Path   string
}

func Open(options Options) (*Store, error) {
	if err := checkDriver(options.Driver); err != nil {
		return nil, err
	}
	return New(options.Path)
}

// checkDriver refuses a database driver other than SQLite, the only one
// there is; an empty driver is SQLite.
func checkDriver(driver string) error {
	if driver != "" && driver != "sqlite" {
		return fmt.Errorf("database driver %q is not supported", driver)
	}
	return nil
}
