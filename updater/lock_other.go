//go:build !darwin && !linux && !windows

package updater

import "errors"

func acquireLock(string) (func(), error) {
	return nil, errors.New("updates are unsupported on this operating system")
}
