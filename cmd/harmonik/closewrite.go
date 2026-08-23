package main

import (
	"errors"
	"syscall"
)

func isBenignCloseWrite(err error) bool {
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.ENOTCONN) || errors.Is(err, syscall.EPIPE)
}
