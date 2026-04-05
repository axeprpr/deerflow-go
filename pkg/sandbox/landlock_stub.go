//go:build !linux

package sandbox

func CheckLandlockAvailable() bool {
	return false
}

func probeLandlock(string) error {
	return nil
}

func applyLandlock(string) error {
	return nil
}
