package desktop

import (
	"io"
	"os"
	"path/filepath"
)

func copyFileTemp(src, pattern string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

func copyDirFilesTemp(src, pattern string) (string, error) {
	dst, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		os.RemoveAll(dst)
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyNamed(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			os.RemoveAll(dst)
			return "", err
		}
	}
	return dst, nil
}

func copyNamed(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
