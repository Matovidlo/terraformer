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
	"testing"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6"
	"github.com/zclconf/go-cty/cty"
)

func TestConvertSchemaBlockToType_Simple(t *testing.T) {
	// Create a simple schema with string and number attributes
	stringType, _ := json.Marshal(cty.String)
	numberType, _ := json.Marshal(cty.Number)
	boolType, _ := json.Marshal(cty.Bool)

	block := &tfplugin6.Schema_Block{
		Attributes: []*tfplugin6.Schema_Attribute{
			{
				Name: "id",
				Type: stringType,
			},
			{
				Name: "name",
				Type: stringType,
			},
			{
				Name: "count",
				Type: numberType,
			},
			{
				Name: "enabled",
				Type: boolType,
			},
		},
	}

	result, err := ConvertSchemaBlockToType(block)
	if err != nil {
		t.Fatalf("ConvertSchemaBlockToType failed: %v", err)
	}

	if !result.IsObjectType() {
		t.Errorf("Expected object type, got %s", result.FriendlyName())
	}

	attrTypes := result.AttributeTypes()
	if len(attrTypes) != 4 {
		t.Errorf("Expected 4 attributes, got %d", len(attrTypes))
	}

	// Verify attribute types
	if attrTypes["id"] != cty.String {
		t.Errorf("Expected id to be String, got %s", attrTypes["id"].FriendlyName())
	}
	if attrTypes["name"] != cty.String {
		t.Errorf("Expected name to be String, got %s", attrTypes["name"].FriendlyName())
	}
	if attrTypes["count"] != cty.Number {
		t.Errorf("Expected count to be Number, got %s", attrTypes["count"].FriendlyName())
	}
	if attrTypes["enabled"] != cty.Bool {
		t.Errorf("Expected enabled to be Bool, got %s", attrTypes["enabled"].FriendlyName())
	}
}

func TestConvertSchemaBlockToType_WithNestedBlock(t *testing.T) {
	// Create a schema with a nested list block
	stringType, _ := json.Marshal(cty.String)

	nestedBlock := &tfplugin6.Schema_Block{
		Attributes: []*tfplugin6.Schema_Attribute{
			{
				Name: "key",
				Type: stringType,
			},
			{
				Name: "value",
				Type: stringType,
			},
		},
	}

	block := &tfplugin6.Schema_Block{
		Attributes: []*tfplugin6.Schema_Attribute{
			{
				Name: "id",
				Type: stringType,
			},
		},
		BlockTypes: []*tfplugin6.Schema_NestedBlock{
			{
				TypeName: "tags",
				Block:    nestedBlock,
				Nesting:  tfplugin6.Schema_NestedBlock_LIST,
			},
		},
	}

	result, err := ConvertSchemaBlockToType(block)
	if err != nil {
		t.Fatalf("ConvertSchemaBlockToType failed: %v", err)
	}

	if !result.IsObjectType() {
		t.Errorf("Expected object type, got %s", result.FriendlyName())
	}

	attrTypes := result.AttributeTypes()
	if len(attrTypes) != 2 {
		t.Errorf("Expected 2 attributes, got %d", len(attrTypes))
	}

	// Verify tags is a list type
	tagsType := attrTypes["tags"]
	if !tagsType.IsListType() {
		t.Errorf("Expected tags to be List, got %s", tagsType.FriendlyName())
	}

	// Verify the element type of the list
	elemType := tagsType.ElementType()
	if !elemType.IsObjectType() {
		t.Errorf("Expected tags element to be Object, got %s", elemType.FriendlyName())
	}

	elemAttrs := elemType.AttributeTypes()
	if len(elemAttrs) != 2 {
		t.Errorf("Expected 2 nested attributes, got %d", len(elemAttrs))
	}
}

func TestConvertSchemaBlockToType_WithNestedObject(t *testing.T) {
	// Create a schema with nested object (ListNestedAttribute style)
	stringType, _ := json.Marshal(cty.String)
	numberType, _ := json.Marshal(cty.Number)

	nestedAttr := &tfplugin6.Schema_Attribute{
		Name: "config",
		NestedType: &tfplugin6.Schema_Object{
			Attributes: []*tfplugin6.Schema_Attribute{
				{
					Name: "host",
					Type: stringType,
				},
				{
					Name: "port",
					Type: numberType,
				},
			},
			Nesting: tfplugin6.Schema_Object_SINGLE,
		},
	}

	block := &tfplugin6.Schema_Block{
		Attributes: []*tfplugin6.Schema_Attribute{
			{
				Name: "id",
				Type: stringType,
			},
			nestedAttr,
		},
	}

	result, err := ConvertSchemaBlockToType(block)
	if err != nil {
		t.Fatalf("ConvertSchemaBlockToType failed: %v", err)
	}

	attrTypes := result.AttributeTypes()
	if len(attrTypes) != 2 {
		t.Errorf("Expected 2 attributes, got %d", len(attrTypes))
	}

	// Verify config is an object
	configType := attrTypes["config"]
	if !configType.IsObjectType() {
		t.Errorf("Expected config to be Object, got %s", configType.FriendlyName())
	}

	configAttrs := configType.AttributeTypes()
	if len(configAttrs) != 2 {
		t.Errorf("Expected 2 config attributes, got %d", len(configAttrs))
	}
	if configAttrs["host"] != cty.String {
		t.Errorf("Expected host to be String, got %s", configAttrs["host"].FriendlyName())
	}
	if configAttrs["port"] != cty.Number {
		t.Errorf("Expected port to be Number, got %s", configAttrs["port"].FriendlyName())
	}
}

func TestConvertSchemaBlockToType_NilBlock(t *testing.T) {
	_, err := ConvertSchemaBlockToType(nil)
	if err == nil {
		t.Error("Expected error for nil block, got nil")
	}
}

func TestDecodeAttributeType_EmptyType(t *testing.T) {
	attr := &tfplugin6.Schema_Attribute{
		Name: "test",
		Type: []byte{},
	}

	_, err := decodeAttributeType(attr)
	if err == nil {
		t.Error("Expected error for empty type, got nil")
	}
}

func TestConvertNestedBlock_AllNestingModes(t *testing.T) {
	stringType, _ := json.Marshal(cty.String)

	nestingModes := []tfplugin6.Schema_NestedBlock_NestingMode{
		tfplugin6.Schema_NestedBlock_SINGLE,
		tfplugin6.Schema_NestedBlock_LIST,
		tfplugin6.Schema_NestedBlock_SET,
		tfplugin6.Schema_NestedBlock_MAP,
		tfplugin6.Schema_NestedBlock_GROUP,
	}

	for _, mode := range nestingModes {
		nestedBlock := &tfplugin6.Schema_NestedBlock{
			TypeName: "test_block",
			Block: &tfplugin6.Schema_Block{
				Attributes: []*tfplugin6.Schema_Attribute{
					{Name: "value", Type: stringType},
				},
			},
			Nesting: mode,
		}

		result, err := convertNestedBlock(nestedBlock)
		if err != nil {
			t.Errorf("Failed to convert nesting mode %v: %v", mode, err)
			continue
		}

		// Verify the result type matches the nesting mode
		switch mode {
		case tfplugin6.Schema_NestedBlock_SINGLE, tfplugin6.Schema_NestedBlock_GROUP:
			if !result.IsObjectType() {
				t.Errorf("Expected Object for mode %v, got %s", mode, result.FriendlyName())
			}
		case tfplugin6.Schema_NestedBlock_LIST:
			if !result.IsListType() {
				t.Errorf("Expected List for mode %v, got %s", mode, result.FriendlyName())
			}
		case tfplugin6.Schema_NestedBlock_SET:
			if !result.IsSetType() {
				t.Errorf("Expected Set for mode %v, got %s", mode, result.FriendlyName())
			}
		case tfplugin6.Schema_NestedBlock_MAP:
			if !result.IsMapType() {
				t.Errorf("Expected Map for mode %v, got %s", mode, result.FriendlyName())
			}
		}
	}
}
