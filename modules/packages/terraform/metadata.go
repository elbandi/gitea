// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package terraform

import (
	"strings"

	packages_model "gitea.dev/models/packages"
)

const (
	PropertyOS   = "terraform.os"
	PropertyArch = "terraform.arch"

	Sha256SumsFileKey    = "sha256sums"
	Sha256SumsSigFileKey = "sha256sums_sig"
)

func GetCompositeKey(filename string) string {
	switch {
	case strings.HasSuffix(filename, "SHA256SUMS"):
		return Sha256SumsFileKey
	case strings.HasSuffix(filename, "SHA256SUMS.sig"):
		return Sha256SumsSigFileKey
	default:
		return packages_model.EmptyFileKey
	}
}
