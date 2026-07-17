package eval

import (
	"errors"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// DecompressZstdFile writes a new output file through the pinned pure-Go zstd
// decoder. It refuses to overwrite an existing artifact.
func DecompressZstdFile(inputPath, outputPath string) error {
	if inputPath == "" || outputPath == "" {
		return errors.New("zstd input and output paths are required")
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = output.Close()
		if !success {
			_ = os.Remove(outputPath)
		}
	}()

	decoder, err := zstd.NewReader(input)
	if err != nil {
		return err
	}
	defer decoder.Close()
	if _, err := io.Copy(output, decoder); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	success = true
	return nil
}
