package store

import "fmt"

type Options struct {
	Driver string
	Path   string
}

func Open(options Options) (*Store, error) {
	driver := options.Driver
	if driver == "" {
		driver = "sqlite"
	}
	if driver != "sqlite" {
		return nil, fmt.Errorf("database driver %q is not supported", driver)
	}
	return New(options.Path)
}
