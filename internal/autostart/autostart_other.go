//go:build !darwin && !linux

package autostart

func supported() bool { return false }
func enable() error   { return ErrUnsupported }
func disable() error  { return nil }
func enabled() bool   { return false }
