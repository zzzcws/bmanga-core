//go:build !windows

package prototype

import "os"

func fileInfoIsReparsePoint(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
}
