// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package fromproto

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6"
)

func RawState(in *tfplugin6.RawState) *tfprotov6.RawState {
	if in == nil {
		return nil
	}

	resp := &tfprotov6.RawState{
		JSON:    in.Json,
		Flatmap: in.Flatmap,
	}

	return resp
}
