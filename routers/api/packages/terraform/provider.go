// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package terraform

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	packages_model "gitea.dev/models/packages"
	packages_module "gitea.dev/modules/packages"
	terraform_module "gitea.dev/modules/packages/terraform"
	"gitea.dev/modules/util"
	"gitea.dev/routers/api/packages/helper"
	"gitea.dev/services/context"
	packages_service "gitea.dev/services/packages"
	terraform_service "gitea.dev/services/packages/terraform"
)

func GetProviderVersions(ctx *context.Context) {
	p, err := packages_model.GetPackageByName(ctx, ctx.Package.Owner.ID, packages_model.TypeTerraformProvider, ctx.PathParam("packagename"))
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			apiError(ctx, http.StatusNotFound, err)
		} else {
			apiError(ctx, http.StatusInternalServerError, err)
		}
		return
	}
	b, err := terraform_service.BuildVersions(ctx, p)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	if b == nil {
		apiError(ctx, http.StatusNotFound, nil)
		return
	}

	ctx.JSON(http.StatusOK, b)
}

func GetProviderDownload(ctx *context.Context) {
	packageName := ctx.PathParam("packagename")
	packageVersion := ctx.PathParam("packageversion")

	pv, err := packages_model.GetVersionByNameAndVersion(ctx, ctx.Package.Owner.ID, packages_model.TypeTerraformProvider, packageName, packageVersion)
	if err != nil {
		if errors.Is(err, packages_model.ErrPackageNotExist) {
			apiError(ctx, http.StatusNotFound, err)
			return
		}
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	pd, err := packages_model.GetPackageDescriptor(ctx, pv)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	osname := ctx.PathParam("os")
	architecture := ctx.PathParam("architecture")
	pfs, _, err := packages_model.SearchFiles(ctx, &packages_model.PackageFileSearchOptions{
		VersionID:    pv.ID,
		CompositeKey: packages_model.EmptyFileKey,
		Properties: map[string]string{
			terraform_module.PropertyOS:   osname,
			terraform_module.PropertyArch: architecture,
		},
	})
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	if len(pfs) != 1 {
		apiError(ctx, http.StatusNotFound, nil)
		return
	}
	pfd, err := packages_model.GetPackageFileDescriptor(ctx, pfs[0])
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	b, err := terraform_service.BuildDownload(ctx, pd, pfd)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	ctx.JSON(http.StatusOK, b)
}

// DownloadProviderFile serves the specific terraform provider.
func DownloadProviderFile(ctx *context.Context) {
	s, u, pf, err := packages_service.OpenFileForDownloadByPackageNameAndVersion(
		ctx,
		&packages_service.PackageInfo{
			Owner:       ctx.Package.Owner,
			PackageType: packages_model.TypeTerraformProvider,
			Name:        ctx.PathParam("packagename"),
			Version:     ctx.PathParam("packageversion"),
		},
		&packages_service.PackageFileInfo{
			Filename:     ctx.PathParam("filename"),
			CompositeKey: terraform_module.GetCompositeKey(ctx.PathParam("filename")),
		},
		ctx.Req.Method,
	)
	if err != nil {
		if errors.Is(err, packages_model.ErrPackageNotExist) || errors.Is(err, packages_model.ErrPackageFileNotExist) {
			apiError(ctx, http.StatusNotFound, err)
			return
		}
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	helper.ServePackageFile(ctx, s, u, pf)
}

func SaveArchFile(ctx *context.Context, pv *packages_model.PackageVersion, filename, compositeKey string) error {
	// split filename with _ delimiter, and expect 3 parts: {packagename}_{packageversion}_{os}_{arch}
	parts := strings.SplitN(strings.TrimSuffix(filename, ".zip"), "_", 4)
	if len(parts) != 4 {
		return fmt.Errorf("invalid file name: %s", filename)
	}
	return SaveFile(ctx, pv, filename, compositeKey, map[string]string{
		terraform_module.PropertyOS:   parts[2],
		terraform_module.PropertyArch: parts[3],
	})
}

func SaveFile(ctx *context.Context, pv *packages_model.PackageVersion, filename, compositeKey string, properties map[string]string) error {
	file, err := ctx.FormFileOptionalReadCloser(filename)
	if file == nil || err != nil {
		return fmt.Errorf("unable to read %s file: %w", filename, err)
	}
	defer file.Close()

	buf, err := packages_module.CreateHashedBufferFromReader(file)
	if err != nil {
		return err
	}
	defer buf.Close()

	_, err = packages_service.AddFileToPackageVersionInternal(
		ctx,
		pv,
		&packages_service.PackageFileCreationInfo{
			PackageFileInfo: packages_service.PackageFileInfo{
				Filename:     filename,
				CompositeKey: compositeKey,
			},
			Properties: properties,
			Data:       buf,
		},
	)
	return err
}

// UploadProvider uploads the specific terraform provider.
// Duplicated packages get rejected.
func UploadProvider(ctx *context.Context) {
	packageName := ctx.PathParam("packagename")

	if !isValidPackageName(packageName) {
		apiError(ctx, http.StatusBadRequest, errors.New("invalid package name"))
		return
	}

	packageVersion := ctx.PathParam("packageversion")
	if packageVersion != strings.TrimSpace(packageVersion) {
		apiError(ctx, http.StatusBadRequest, errors.New("invalid package version"))
		return
	}

	shaSumsFileName := fmt.Sprintf("terraform-provider-%s_%s_SHA256SUMS", packageName, packageVersion)
	file, err := ctx.FormFileOptionalReadCloser(shaSumsFileName)
	if file == nil || err != nil {
		apiError(ctx, http.StatusBadRequest, "unable to read SHA256SUMS file")
		return
	}
	defer file.Close()

	sha256sumsBuf, err := packages_module.CreateHashedBufferFromReader(file)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	defer sha256sumsBuf.Close()

	data, err := io.ReadAll(sha256sumsBuf)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	checksums, err := terraform_service.ParseChecksums(data)
	if err != nil {
		apiError(ctx, http.StatusBadRequest, fmt.Errorf("failed to parse %s SHA256SUMS file: %w", shaSumsFileName, err))
	}

	for f, h := range checksums {
		err = terraform_service.VerifyFile(ctx, f, h)
		if err != nil {
			apiError(ctx, http.StatusInternalServerError, err)
			return
		}
	}
	if _, err := sha256sumsBuf.Seek(0, io.SeekStart); err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	pv, _, err := packages_service.CreatePackageAndAddFile(
		ctx,
		&packages_service.PackageCreationInfo{
			PackageInfo: packages_service.PackageInfo{
				Owner:       ctx.Package.Owner,
				PackageType: packages_model.TypeTerraformProvider,
				Name:        packageName,
				Version:     packageVersion,
			},
			Creator: ctx.Doer,
		},
		&packages_service.PackageFileCreationInfo{
			PackageFileInfo: packages_service.PackageFileInfo{
				Filename:     shaSumsFileName,
				CompositeKey: terraform_module.Sha256SumsFileKey,
			},
			Creator: ctx.Doer,
			Data:    sha256sumsBuf,
			IsLead:  true,
		},
	)
	if err != nil {
		switch err {
		case packages_model.ErrDuplicatePackageVersion:
			apiError(ctx, http.StatusConflict, err)
		case packages_service.ErrQuotaTotalCount, packages_service.ErrQuotaTypeSize, packages_service.ErrQuotaTotalSize:
			apiError(ctx, http.StatusForbidden, err)
		default:
			apiError(ctx, http.StatusInternalServerError, err)
		}
		return
	}

	err = SaveFile(ctx, pv, shaSumsFileName+".sig", terraform_module.Sha256SumsSigFileKey, nil)
	if err != nil {
		switch err {
		case packages_service.ErrQuotaTotalCount, packages_service.ErrQuotaTypeSize, packages_service.ErrQuotaTotalSize:
			apiError(ctx, http.StatusForbidden, err)
		default:
			apiError(ctx, http.StatusInternalServerError, err)
		}
		// delete provider!
		return
	}

	for f := range checksums {
		err = SaveArchFile(ctx, pv, f, packages_model.EmptyFileKey)
		if err != nil {
			switch err {
			case packages_service.ErrQuotaTotalCount, packages_service.ErrQuotaTypeSize, packages_service.ErrQuotaTotalSize:
				apiError(ctx, http.StatusForbidden, err)
			default:
				apiError(ctx, http.StatusInternalServerError, err)
			}
			// delete provider!
			return
		}
	}

	ctx.Status(http.StatusCreated)
}

// DeleteProvider deletes the specific terraform provider.
func DeleteProvider(ctx *context.Context) {
	err := packages_service.RemovePackageVersionByNameAndVersion(
		ctx,
		ctx.Doer,
		&packages_service.PackageInfo{
			Owner:       ctx.Package.Owner,
			PackageType: packages_model.TypeTerraformProvider,
			Name:        ctx.PathParam("packagename"),
			Version:     ctx.PathParam("packageversion"),
		},
	)
	if err != nil {
		if errors.Is(err, packages_model.ErrPackageNotExist) {
			apiError(ctx, http.StatusNotFound, err)
			return
		}
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}
