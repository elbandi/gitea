// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package terraform

import (
	"bytes"
	"context"
	"fmt"

	asymkey_model "gitea.dev/models/asymkey"
	packages_model "gitea.dev/models/packages"
	terraform_module "gitea.dev/modules/packages/terraform"
	"gitea.dev/modules/setting"
	asymkey_service "gitea.dev/services/asymkey"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

type DownloadResponse struct {
	Protocols           []string            `json:"protocols"`
	OS                  string              `json:"os"`
	Arch                string              `json:"arch"`
	Filename            string              `json:"filename"`
	DownloadURL         string              `json:"download_url"`
	SHASumsURL          string              `json:"shasums_url"`
	SHASumsSignatureURL string              `json:"shasums_signature_url"`
	SHASum              string              `json:"shasum"`
	SigningKeys         SigningKeysResponse `json:"signing_keys"`
}

type SigningKeysResponse struct {
	GPGPublicKeys []GPGPublicKey `json:"gpg_public_keys"`
}

type GPGPublicKey struct {
	KeyID          string `json:"key_id"`
	ASCIIArmor     string `json:"ascii_armor"`
	TrustSignature string `json:"trust_signature"`
	Source         string `json:"source"`
	SourceURL      any    `json:"source_url"`
}

func serializeGPGKey(ctx context.Context, key *asymkey_model.GPGKey) (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	e, err := asymkey_model.GPGKeyToEntity(ctx, key)
	if err != nil {
		return "", err
	}
	if err := e.Serialize(w); err != nil {
		return "", err
	}
	w.Close()
	return buf.String(), nil
}

func BuildDownload(ctx context.Context, pd *packages_model.PackageDescriptor, pfd *packages_model.PackageFileDescriptor) (DownloadResponse, error) {
	base := fmt.Sprintf("%s/api/packages/%s/terraform/provider/%s/%s", setting.AppURL, pd.Owner.Name, pd.Package.Name, pd.Version.Version)
	keys, err := asymkey_service.GetUserPubkeysGPG(ctx, pd.Creator.ID)
	if err != nil {
		return DownloadResponse{}, err
	}
	pubKeys := make([]GPGPublicKey, 0, len(keys))
	for _, key := range keys {
		buf, err := serializeGPGKey(ctx, key)
		if err != nil {
			return DownloadResponse{}, err
		}
		pubKeys = append(pubKeys, GPGPublicKey{
			KeyID:      key.PaddedKeyID(),
			ASCIIArmor: buf,
			Source:     "",
			SourceURL:  nil,
		})
	}

	response := DownloadResponse{
		Protocols: []string{"5.0"},
		OS:        pfd.Properties.GetByName(terraform_module.PropertyOS),
		Arch:      pfd.Properties.GetByName(terraform_module.PropertyArch),
		Filename:  pfd.File.Name,
		DownloadURL: fmt.Sprintf("%s/%s",
			base,
			pfd.File.Name,
		),
		SHASumsURL: fmt.Sprintf("%s/terraform-provider-%s_%s_SHA256SUMS",
			base,
			pd.Package.Name, pd.Version.Version,
		),
		SHASumsSignatureURL: fmt.Sprintf("%s/terraform-provider-%s_%s_SHA256SUMS.sig",
			base,
			pd.Package.Name, pd.Version.Version,
		),
		SHASum: pfd.Blob.HashSHA256,
		SigningKeys: SigningKeysResponse{
			GPGPublicKeys: pubKeys,
		},
	}
	return response, nil
}
