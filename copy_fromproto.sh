#!/bin/bash
# Script to copy fromproto files with updated import paths

SRC_DIR=~/go/pkg/mod/github.com/hashicorp/terraform-plugin-go@v0.29.0/tfprotov6/internal/fromproto
DST_DIR=terraformutils/providerwrapper/internal/fromproto

# List of files to copy (excluding test files)
FILES="action.go client_capabilities.go data_source.go doc.go dynamic_value.go ephemeral_resource.go function.go list_resource.go provider.go raw_state.go resource.go resource_identity_data.go"

for f in $FILES; do
    echo "Processing $f..."
    sed 's|github.com/hashicorp/terraform-plugin-go/tfprotov6/internal/tfplugin6|github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6|g' "$SRC_DIR/$f" > "$DST_DIR/$f"
done

echo "Done! Copied $(echo $FILES | wc -w) files."
