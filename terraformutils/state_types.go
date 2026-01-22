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

// InstanceInfo represents metadata about a resource instance.
// This is a lightweight replacement for terraform.InstanceInfo from the old SDK.
type InstanceInfo struct {
	// Type is the resource type (e.g., "aws_instance", "google_compute_instance")
	Type string

	// Id is the unique identifier for this resource instance
	// Format: "<type>.<name>" (e.g., "aws_instance.web_server")
	Id string
}

// InstanceState represents the state of a resource instance.
// This is a lightweight replacement for terraform.InstanceState from the old SDK.
type InstanceState struct {
	// ID is the resource's unique identifier in the provider
	ID string

	// Attributes is a flat map of all resource attributes
	// Keys use dot notation for nested values (e.g., "tags.Name", "network.0.cidr")
	Attributes map[string]string
}

// OutputState represents a Terraform output value.
// This is a lightweight replacement for terraform.OutputState from the old SDK.
type OutputState struct {
	// Value is the output value (can be any type)
	Value interface{}

	// Type describes the output type (optional, for compatibility)
	Type string

	// Sensitive indicates if the output contains sensitive data
	Sensitive bool
}

// State represents a Terraform state file.
// This is a lightweight replacement for terraform.State from the old SDK.
type State struct {
	Version   int            `json:"version"`
	TFVersion string         `json:"terraform_version"`
	Serial    int64          `json:"serial"`
	Modules   []*ModuleState `json:"modules"`
}

// ModuleState represents a module's state in a Terraform state file.
// This is a lightweight replacement for terraform.ModuleState from the old SDK.
type ModuleState struct {
	Path      []string                  `json:"path"`
	Outputs   map[string]*OutputState   `json:"outputs"`
	Resources map[string]*ResourceState `json:"resources"`
}

// ResourceState represents a resource's state in a Terraform state file.
// This is a lightweight replacement for terraform.ResourceState from the old SDK.
type ResourceState struct {
	Type     string         `json:"type"`
	Primary  *InstanceState `json:"primary"`
	Provider string         `json:"provider"`
}
