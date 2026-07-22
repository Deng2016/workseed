//go:build !windows && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package main

func acquireSingleInstance() (release func(), acquired bool, err error) {
	return func() {}, true, nil
}
