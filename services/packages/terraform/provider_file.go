package terraform

import (
	"bytes"
	"fmt"

	packages_module "gitea.dev/modules/packages"
	"gitea.dev/services/context"
)

func VerifyFile(ctx *context.Context, fileName string, h SHA256Hash) error {
	file, err := ctx.FormFileOptionalReadCloser(fileName)
	if file == nil || err != nil {
		return fmt.Errorf("unable to read %s file", fileName)
	}
	defer file.Close()

	buf, err := packages_module.CreateHashedBufferFromReader(file)
	if err != nil {
		return err
	}
	defer buf.Close()

	_, _, hash, _ := buf.Sums()
	if !bytes.Equal(hash, h[:]) {
		return ErrInvalidSHA256Hash
	}
	return nil
}
