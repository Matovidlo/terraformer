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

package terraformutils

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper"
	"github.com/zclconf/go-cty/cty"
)

type Resource struct {
	InstanceInfo      *InstanceInfo
	InstanceState     *InstanceState
	Outputs           map[string]*OutputState `json:",omitempty"`
	ResourceName      string
	Provider          string
	Item              map[string]interface{} `json:",omitempty"`
	IgnoreKeys        []string               `json:",omitempty"`
	AllowEmptyValues  []string               `json:",omitempty"`
	AdditionalFields  map[string]interface{} `json:",omitempty"`
	SlowQueryRequired bool
	DataFiles         map[string][]byte
}

type ApplicableFilter interface {
	IsApplicable(resourceName string) bool
}

type ResourceFilter struct {
	ApplicableFilter
	ServiceName      string
	FieldPath        string
	AcceptableValues []string
}

func (rf *ResourceFilter) Filter(resource Resource) bool {
	if !rf.IsApplicable(strings.TrimPrefix(resource.InstanceInfo.Type, resource.Provider+"_")) {
		return true
	}
	var vals []interface{}
	switch {
	case rf.FieldPath == "id":
		vals = []interface{}{resource.InstanceState.ID}
	case rf.AcceptableValues == nil:
		var hasField = WalkAndCheckField(rf.FieldPath, resource.InstanceState.Attributes)
		if hasField {
			return true
		}
		return WalkAndCheckField(rf.FieldPath, resource.Item)
	default:
		vals = WalkAndGet(rf.FieldPath, resource.InstanceState.Attributes)
		if len(vals) == 0 {
			vals = WalkAndGet(rf.FieldPath, resource.Item)
		}
	}
	for _, val := range vals {
		for _, acceptableValue := range rf.AcceptableValues {
			if val == acceptableValue {
				return true
			}
		}
	}
	return false
}

func (rf *ResourceFilter) IsApplicable(serviceName string) bool {
	return rf.ServiceName == "" || rf.ServiceName == serviceName
}

func (rf *ResourceFilter) isInitial() bool {
	return rf.FieldPath == "id"
}

func NewResource(id, resourceName, resourceType, provider string,
	attributes map[string]string,
	allowEmptyValues []string,
	additionalFields map[string]interface{}) Resource {
	return Resource{
		ResourceName: TfSanitize(resourceName),
		Item:         nil,
		Provider:     provider,
		InstanceState: &InstanceState{
			ID:         id,
			Attributes: attributes,
		},
		InstanceInfo: &InstanceInfo{
			Type: resourceType,
			Id:   fmt.Sprintf("%s.%s", resourceType, TfSanitize(resourceName)),
		},
		AdditionalFields: additionalFields,
		AllowEmptyValues: allowEmptyValues,
	}
}

func NewSimpleResource(id, resourceName, resourceType, provider string, allowEmptyValues []string) Resource {
	return NewResource(
		id,
		resourceName,
		resourceType,
		provider,
		map[string]string{},
		allowEmptyValues,
		map[string]interface{}{},
	)
}

func (r *Resource) Refresh(provider *providerwrapper.ProviderWrapper) {
	if r.SlowQueryRequired {
		time.Sleep(200 * time.Millisecond)
	}

	// Convert InstanceState attributes to cty.Value for the provider
	priorState, err := attributesToCtyValue(r.InstanceState.Attributes)
	if err != nil {
		log.Printf("Error converting attributes to cty.Value: %v", err)
		return
	}

	// Call provider Refresh
	newState, err := provider.Refresh(r.InstanceInfo.Type, r.InstanceState.ID, priorState, 0)
	if err != nil {
		log.Println(err)
		return
	}

	// Convert cty.Value back to attributes map
	newAttrs, err := ctyValueToAttributes(newState)
	if err != nil {
		log.Printf("Error converting cty.Value to attributes: %v", err)
		return
	}

	r.InstanceState.Attributes = newAttrs
}

// attributesToCtyValue converts a flat map[string]string to a cty.Value
func attributesToCtyValue(attrs map[string]string) (cty.Value, error) {
	if attrs == nil {
		return cty.ObjectVal(map[string]cty.Value{}), nil
	}

	// Convert map[string]string to map[string]cty.Value
	values := make(map[string]cty.Value)
	for k, v := range attrs {
		values[k] = cty.StringVal(v)
	}

	return cty.ObjectVal(values), nil
}

// ctyValueToAttributes converts a cty.Value to a flat map[string]string
func ctyValueToAttributes(val cty.Value) (map[string]string, error) {
	if val.IsNull() {
		return map[string]string{}, nil
	}

	attrs := make(map[string]string)
	if val.Type().IsObjectType() {
		for key := range val.Type().AttributeTypes() {
			attrVal := val.GetAttr(key)
			if !attrVal.IsNull() && attrVal.Type() == cty.String {
				attrs[key] = attrVal.AsString()
			} else if !attrVal.IsNull() {
				// For non-string types, convert to Go value and stringify
				attrs[key] = fmt.Sprintf("%v", attrVal)
			}
		}
	}

	return attrs, nil
}

func (r Resource) GetIDKey() string {
	if _, exist := r.InstanceState.Attributes["self_link"]; exist {
		return "self_link"
	}
	return "id"
}

func (r *Resource) ParseTFstate(parser Flatmapper, impliedType cty.Type) error {
	attributes, err := parser.Parse(impliedType)
	if err != nil {
		return err
	}

	// add Additional Fields to resource
	for key, value := range r.AdditionalFields {
		attributes[key] = value
	}

	if attributes == nil {
		attributes = map[string]interface{}{} // ensure HCL can represent empty resource correctly
	}

	r.Item = attributes
	return nil
}

func (r *Resource) ConvertTFstate(provider *providerwrapper.ProviderWrapper) error {
	ignoreKeys := []*regexp.Regexp{}
	for _, pattern := range r.IgnoreKeys {
		ignoreKeys = append(ignoreKeys, regexp.MustCompile(pattern))
	}
	allowEmptyValues := []*regexp.Regexp{}
	for _, pattern := range r.AllowEmptyValues {
		if pattern != "" {
			allowEmptyValues = append(allowEmptyValues, regexp.MustCompile(pattern))
		}
	}
	parser := NewFlatmapParser(r.InstanceState.Attributes, ignoreKeys, allowEmptyValues)
	impliedType, err := provider.GetResourceImpliedType(r.InstanceInfo.Type)
	if err != nil {
		return err
	}
	return r.ParseTFstate(parser, impliedType)
}

func (r *Resource) ServiceName() string {
	return strings.TrimPrefix(r.InstanceInfo.Type, r.Provider+"_")
}
