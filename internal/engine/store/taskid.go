package store

import (
	"crypto/rand"
	"math/big"
)

const taskIDCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
const taskIDLength = 8

// GenerateTaskID generates an 8-character lowercase alphanumeric ID using crypto/rand.
func GenerateTaskID() string {
	b := make([]byte, taskIDLength)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(taskIDCharset))))
		b[i] = taskIDCharset[n.Int64()]
	}
	return string(b)
}
