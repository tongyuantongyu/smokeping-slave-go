//go:build !windows && !linux

package priority

import "errors"

func Priority() bool {
	return errors.New("unimplemented")
}
