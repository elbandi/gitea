// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package terraform

import (
	"context"
	"fmt"

	packages_model "gitea.dev/models/packages"
	terraform_module "gitea.dev/modules/packages/terraform"
)

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type ProviderVersion struct {
	Version   string     `json:"version"`
	Protocols []string   `json:"protocols"`
	Platforms []Platform `json:"platforms"`
}

type VersionResponse struct {
	Id       string            `json:"id"`
	Versions []ProviderVersion `json:"versions"`
}

func BuildVersions(ctx context.Context, p *packages_model.Package) (*VersionResponse, error) {
	pvs, _, err := packages_model.SearchVersions(ctx, &packages_model.PackageSearchOptions{
		PackageID: p.ID,
		Sort:      packages_model.SortVersionAsc,
	})
	if err != nil {
		return nil, fmt.Errorf("SearchVersions[%s]: %w", p.Name, err)
	}
	if len(pvs) == 0 {
		return nil, nil
	}

	pds, err := packages_model.GetPackageDescriptors(ctx, pvs)
	if err != nil {
		return nil, fmt.Errorf("GetPackageDescriptors[%s]: %w", p.Name, err)
	}

	var versions []ProviderVersion
	for _, pd := range pds {
		platforms := make([]Platform, 0, len(pd.Files))
		for _, f := range pd.Files {
			if f.File.CompositeKey == terraform_module.Sha256SumsFileKey || f.File.CompositeKey == terraform_module.Sha256SumsSigFileKey {
				continue
			}
			os := f.Properties.GetByName(terraform_module.PropertyOS)
			arch := f.Properties.GetByName(terraform_module.PropertyArch)

			// Only add if both os and arch are present
			if os != "" && arch != "" {
				platforms = append(platforms, Platform{
					OS:   os,
					Arch: arch,
				})
			}
		}

		version := ProviderVersion{
			Version:   pd.Version.Version,
			Protocols: []string{"5.0"},
			Platforms: platforms,
		}
		versions = append(versions, version)
	}

	return &VersionResponse{
		Id:       "",
		Versions: versions,
	}, nil
}
