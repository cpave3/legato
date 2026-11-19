package main

import (
	"sync"

	"github.com/cpave3/legato/internal/service"
)

var (
	doctorOnce sync.Once
	doctorInst *service.Doctor
)

// processDoctor is this process's doctor: one, so a spawn reads the tool
// checks the start-up run and earlier spawns left.
func processDoctor() *service.Doctor {
	doctorOnce.Do(func() { doctorInst = service.NewDoctor(service.DoctorOptions{}) })
	return doctorInst
}
