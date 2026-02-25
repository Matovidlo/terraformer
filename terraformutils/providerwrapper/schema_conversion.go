// Copyright 2018 The Terraformer Authors.
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

package providerwrapper

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6"
	"github.com/zclconf/go-cty/cty"
)

// ConvertSchemaBlockToType converts a tfplugin6.Schema_Block to cty.Type
// This is the core function that enables proper HCL generation from terraform state
func ConvertSchemaBlockToType(block *tfplugin6.Schema_Block) (cty.Type, error) {
	if block == nil {
		return cty.NilType, fmt.Errorf("schema block is nil")
	}

	attrTypes := make(map[string]cty.Type)

	// Convert simple attributes
	for _, attr := range block.Attributes {
		attrType, err := decodeAttributeType(attr)
		if err != nil {
			// Log warning but continue with DynamicPseudoType for this attribute
			// Partial success is better than total failure
			log.Printf("[WARN] Failed to decode attribute %s: %v, using cty.DynamicPseudoType", attr.Name, err)
			attrTypes[attr.Name] = cty.DynamicPseudoType
			continue
		}
		attrTypes[attr.Name] = attrType
	}

	// Convert nested blocks
	for _, nestedBlock := range block.BlockTypes {
		blockType, err := convertNestedBlock(nestedBlock)
		if err != nil {
			log.Printf("[WARN] Failed to convert nested block %s: %v, using cty.DynamicPseudoType", nestedBlock.TypeName, err)
			attrTypes[nestedBlock.TypeName] = cty.DynamicPseudoType
			continue
		}
		attrTypes[nestedBlock.TypeName] = blockType
	}

	return cty.Object(attrTypes), nil
}

// decodeAttributeType decodes the type bytes to cty.Type
func decodeAttributeType(attr *tfplugin6.Schema_Attribute) (cty.Type, error) {
	// Handle nested_type if present (for ListNestedAttribute, etc.)
	if attr.NestedType != nil {
		return convertNestedObject(attr.NestedType)
	}

	// The type field contains JSON-encoded cty.Type
	// This is the standard format used by terraform-plugin-go
	if len(attr.Type) == 0 {
		return cty.NilType, fmt.Errorf("attribute type is empty")
	}

	// Decode JSON to cty.Type
	var typ cty.Type
	err := json.Unmarshal(attr.Type, &typ)
	if err != nil {
		return cty.NilType, fmt.Errorf("failed to unmarshal type JSON: %w", err)
	}

	return typ, nil
}

// convertNestedObject converts Schema_Object to cty.Type
func convertNestedObject(obj *tfplugin6.Schema_Object) (cty.Type, error) {
	if obj == nil {
		return cty.NilType, fmt.Errorf("nested object is nil")
	}

	attrTypes := make(map[string]cty.Type)

	for _, attr := range obj.Attributes {
		attrType, err := decodeAttributeType(attr)
		if err != nil {
			log.Printf("[WARN] Failed to decode nested attribute %s: %v, using cty.DynamicPseudoType", attr.Name, err)
			attrTypes[attr.Name] = cty.DynamicPseudoType
			continue
		}
		attrTypes[attr.Name] = attrType
	}

	objType := cty.Object(attrTypes)

	// Wrap in collection type based on nesting mode
	switch obj.Nesting {
	case tfplugin6.Schema_Object_SINGLE:
		return objType, nil
	case tfplugin6.Schema_Object_LIST:
		return cty.List(objType), nil
	case tfplugin6.Schema_Object_SET:
		return cty.Set(objType), nil
	case tfplugin6.Schema_Object_MAP:
		return cty.Map(objType), nil
	default:
		// Default to SINGLE if nesting mode is invalid/unknown
		log.Printf("[WARN] Unknown nesting mode %v for nested object, treating as SINGLE", obj.Nesting)
		return objType, nil
	}
}

// convertNestedBlock converts Schema_NestedBlock to cty.Type
func convertNestedBlock(block *tfplugin6.Schema_NestedBlock) (cty.Type, error) {
	if block == nil {
		return cty.NilType, fmt.Errorf("nested block is nil")
	}

	if block.Block == nil {
		return cty.NilType, fmt.Errorf("nested block.Block is nil")
	}

	blockType, err := ConvertSchemaBlockToType(block.Block)
	if err != nil {
		return cty.NilType, fmt.Errorf("failed to convert block type: %w", err)
	}

	// Wrap based on nesting mode
	switch block.Nesting {
	case tfplugin6.Schema_NestedBlock_SINGLE, tfplugin6.Schema_NestedBlock_GROUP:
		// SINGLE and GROUP both represent a single nested block (not a collection)
		return blockType, nil
	case tfplugin6.Schema_NestedBlock_LIST:
		return cty.List(blockType), nil
	case tfplugin6.Schema_NestedBlock_SET:
		return cty.Set(blockType), nil
	case tfplugin6.Schema_NestedBlock_MAP:
		return cty.Map(blockType), nil
	default:
		// Default to SINGLE if nesting mode is invalid/unknown
		log.Printf("[WARN] Unknown nesting mode %v for nested block %s, treating as SINGLE", block.Nesting, block.TypeName)
		return blockType, nil
	}
}
