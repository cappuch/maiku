package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Archive paths are untrusted even when the archive has the expected checksum.
func archivePath(root, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return "", errors.New("unsafe update archive path")
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("update archive escapes staging directory")
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

func unpack(archive, stage, name, product, goos string) (string, error) {
	if strings.HasSuffix(name, ".exe") {
		info, err := os.Stat(archive)
		if err != nil {
			return "", err
		}
		if info.Size() == 0 {
			return "", errors.New("update executable is empty")
		}
		return archive, os.Chmod(archive, 0755)
	}
	root := filepath.Join(stage, "payload")
	if err := os.Mkdir(root, 0700); err != nil {
		return "", err
	}
	var total int64
	write := func(name string, mode os.FileMode, size int64, r io.Reader) error {
		dest, err := archivePath(root, name)
		if err != nil {
			return err
		}
		if mode.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		if !mode.IsRegular() {
			return fmt.Errorf("unsupported archive entry: %s", name)
		}
		if size < 0 || size > maxDownload-total {
			return errors.New("unpacked update exceeds size limit")
		}
		total += size
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()&0755)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(f, io.LimitReader(r, size+1))
		if copyErr == nil {
			copyErr = f.Sync()
		}
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if n != size {
			return errors.New("invalid archive entry size")
		}
		return closeErr
	}
	if strings.HasSuffix(name, ".zip") {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return "", err
		}
		defer z.Close()
		if len(z.File) > 10000 {
			return "", errors.New("too many archive entries")
		}
		for _, f := range z.File {
			if f.UncompressedSize64 > maxDownload {
				return "", errors.New("archive entry exceeds size limit")
			}
			r, err := f.Open()
			if err != nil {
				return "", err
			}
			err = write(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
			r.Close()
			if err != nil {
				return "", err
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return "", err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for count := 0; ; count++ {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if count >= 10000 {
				return "", errors.New("too many archive entries")
			}
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
				return "", errors.New("links and special files are not allowed in update archives")
			}
			if err := write(h.Name, h.FileInfo().Mode(), h.Size, tr); err != nil {
				return "", err
			}
		}
		// Consume the gzip trailer so truncated/corrupt streams are rejected.
		if _, err := io.Copy(io.Discard, io.LimitReader(gz, maxDownload+1)); err != nil {
			return "", err
		}
	}
	payload := filepath.Join(root, "maiku")
	if goos == "windows" {
		payload += ".exe"
	}
	if product == "desktop" {
		if goos == "darwin" {
			payload = filepath.Join(root, "maiku.app")
			binary := filepath.Join(payload, "Contents", "MacOS", "maiku-desktop")
			if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
				return "", errors.New("update is missing the macOS app executable")
			}
			if err := os.Chmod(binary, 0755); err != nil {
				return "", err
			}
			if _, err := os.Stat(filepath.Join(payload, "Contents", "Info.plist")); err != nil {
				return "", err
			}
			return payload, nil
		}
		payload = filepath.Join(root, strings.TrimSuffix(name, ".tar.gz"))
	}
	info, err := os.Stat(payload)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", errors.New("update is missing its executable")
	}
	return payload, os.Chmod(payload, 0755)
}
