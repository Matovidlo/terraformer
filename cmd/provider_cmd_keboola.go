// Copyright 2025 The Terraformer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"os"

	keboola_terraforming "github.com/GoogleCloudPlatform/terraformer/providers/keboola"
	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"github.com/spf13/cobra"
)

func newCmdKeboolaImporter(options ImportOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keboola",
		Short: "Import current state to Terraform configuration from Keboola",
		Long:  "Import current state to Terraform configuration from Keboola",
		RunE: func(cmd *cobra.Command, args []string) error {
			token := os.Getenv("KEBOOLA_TOKEN")
			host := os.Getenv("KEBOOLA_HOST")
			provider := newKeboolaProvider()
			err := Import(provider, options, []string{token, host})
			if err != nil {
				return err
			}
			return nil
		},
	}

	cmd.AddCommand(listCmd(newKeboolaProvider()))
	baseProviderFlags(cmd.PersistentFlags(), &options, "component_configuration,scheduler", "component_configuration=ID")
	return cmd
}

func newKeboolaProvider() terraformutils.ProviderGenerator {
	return &keboola_terraforming.KeboolaProvider{}
}
