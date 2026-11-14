//go:build !darwin

package service

import "fmt"

func wallHelperOS(mode string, _ []string) int {
	fmt.Println("unknown helper", mode)
	return 2
}
