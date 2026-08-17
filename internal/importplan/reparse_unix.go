//go:build !windows

package importplan

import "os"

func fileInfoIsReparsePoint(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
}
