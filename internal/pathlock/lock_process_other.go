//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package pathlock

import "fmt"

func acquireProcessLock(string) (func(), error) {
	return nil, fmt.Errorf("repository process locks require a supported Unix platform")
}
