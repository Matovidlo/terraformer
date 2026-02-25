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

package providerwrapper //nolint

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/msgpack"
	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"google.golang.org/grpc"
)

// DefaultDataDir is the default directory for storing local data.
const DefaultDataDir = ".terraform"

// DefaultPluginVendorDir is the location in the config directory to look for
// user-added plugin binaries. Terraform only reads from this path if it
// exists, it is never created by terraform.
const DefaultPluginVendorDirV12 = "terraform.d/plugins/" + pluginMachineName

// pluginMachineName is the directory name used in new plugin paths.
const pluginMachineName = runtime.GOOS + "_" + runtime.GOARCH

// Handshake is the HandshakeConfig used to configure clients and servers.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  6,
	MagicCookieKey:   "TF_PLUGIN_MAGIC_COOKIE",
	MagicCookieValue: "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2",
}

// GRPCProviderPlugin is a plugin implementation for protocol v6.
type GRPCProviderPlugin struct{}

// Server is not implemented for client-side usage.
func (p *GRPCProviderPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return nil, fmt.Errorf("terraform-plugin client only implements gRPC clients")
}

// Client is not implemented for client-side usage.
func (p *GRPCProviderPlugin) Client(*plugin.MuxBroker, *rpc.Client) (interface{}, error) {
	return nil, fmt.Errorf("terraform-plugin client only implements gRPC clients")
}

// GRPCServer is not implemented for client-side usage.
func (p *GRPCProviderPlugin) GRPCServer(*plugin.GRPCBroker, *grpc.Server) error {
	return fmt.Errorf("terraform-plugin client only implements gRPC clients")
}

// GRPCClient returns the gRPC client for the provider.
func (p *GRPCProviderPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, conn *grpc.ClientConn) (interface{}, error) {
	return tfplugin6.NewProviderClient(conn), nil
}

// VersionedPlugins is the map of plugins for protocol negotiation.
var VersionedPlugins = map[int]plugin.PluginSet{
	6: {
		"provider": &GRPCProviderPlugin{},
	},
}

type ProviderWrapper struct {
	ProviderClient     tfplugin6.ProviderClient
	client             *plugin.Client
	rpcClient          plugin.ClientProtocol
	providerName       string
	config             cty.Value
	cachedSchemaResp   *tfplugin6.GetProviderSchema_Response // Store gRPC response
	retryCount         int
	retrySleepMs       int
}

func NewProviderWrapper(providerName string, providerConfig cty.Value, verbose bool, options ...map[string]int) (*ProviderWrapper, error) {
	p := &ProviderWrapper{retryCount: 5, retrySleepMs: 300}
	p.providerName = providerName
	p.config = providerConfig

	if len(options) > 0 {
		retryCount, hasOption := options[0]["retryCount"]
		if hasOption {
			p.retryCount = retryCount
		}
		retrySleepMs, hasOption := options[0]["retrySleepMs"]
		if hasOption {
			p.retrySleepMs = retrySleepMs
		}
	}

	err := p.initProvider(verbose)

	return p, err
}

func (p *ProviderWrapper) Kill() {
	if p.client != nil {
		p.client.Kill()
	}
}

// getProviderSchemaResponse fetches and caches the provider schema (gRPC response)
func (p *ProviderWrapper) GetProviderSchemaResponse() (*tfplugin6.GetProviderSchema_Response, error) {
	if p.cachedSchemaResp == nil {
		log.Println("[DEBUG] ProviderWrapper: Fetching provider schema from provider")
		if p.ProviderClient == nil {
			return nil, fmt.Errorf("ProviderClient is nil")
		}

		resp, err := p.ProviderClient.GetProviderSchema(context.Background(), &tfplugin6.GetProviderSchema_Request{})
		if err != nil {
			return nil, fmt.Errorf("GetProviderSchema RPC call failed: %w", err)
		}

		if resp.Diagnostics != nil && len(resp.Diagnostics) > 0 {
			for _, diag := range resp.Diagnostics {
				log.Printf("[WARN] ProviderWrapper: Diagnostic from GetProviderSchema: %s: %s\n", diag.Summary, diag.Detail)
			}
		}

		p.cachedSchemaResp = resp
		log.Println("[DEBUG] ProviderWrapper: Provider schema cached successfully")
	}
	return p.cachedSchemaResp, nil
}

// GetSchema returns the provider schema (for backward compatibility)
// Deprecated: Use getProviderSchemaResponse for new code
func (p *ProviderWrapper) GetSchema() *tfprotov6.Schema {
	resp, err := p.GetProviderSchemaResponse()
	if err != nil {
		log.Printf("[ERROR] ProviderWrapper: GetSchema failed: %v\n", err)
		return nil
	}
	if resp.Provider == nil {
		return nil
	}
	// Convert tfplugin6.Schema to tfprotov6.Schema using fromproto
	// Note: fromproto doesn't have Schema conversion, we'll need to handle this differently
	// For now, return nil and handle at the call sites
	log.Println("[WARN] GetSchema: Schema conversion not yet implemented, returning nil")
	return nil
}

// GetResourceSchema returns the schema for a specific resource type (gRPC version)
func (p *ProviderWrapper) GetResourceSchema(ctx context.Context, typeName string) (*tfplugin6.Schema, error) {
	resp, err := p.GetProviderSchemaResponse()
	if err != nil {
		return nil, fmt.Errorf("get schema: %w", err)
	}

	rs, ok := resp.ResourceSchemas[typeName]
	if !ok {
		return nil, fmt.Errorf("resource type %q not found in provider schema", typeName)
	}
	return rs, nil
}

// GetResourceImpliedType returns a cty.Type for a given resource type
// This converts the provider's schema to a proper cty.Type for HCL generation
func (p *ProviderWrapper) GetResourceImpliedType(typeName string) (cty.Type, error) {
	// Get the resource schema
	schema, err := p.GetResourceSchema(context.Background(), typeName)
	if err != nil {
		return cty.NilType, fmt.Errorf("get schema for %s: %w", typeName, err)
	}

	if schema == nil || schema.Block == nil {
		return cty.NilType, fmt.Errorf("schema or block is nil for %s", typeName)
	}

	// Convert schema block to cty.Type
	impliedType, err := ConvertSchemaBlockToType(schema.Block)
	if err != nil {
		return cty.NilType, fmt.Errorf("convert schema to cty.Type for %s: %w", typeName, err)
	}

	return impliedType, nil
}

func (p *ProviderWrapper) GetReadOnlyAttributes(resourceTypes []string) (map[string][]string, error) {
	readOnlyAttributes := make(map[string][]string)

	for _, resourceName := range resourceTypes {
		schema, err := p.GetResourceSchema(context.Background(), resourceName)
		if err != nil {
			log.Printf("[WARN] Could not get schema for resource type %s: %v", resourceName, err)
			continue
		}
		if schema == nil || schema.Block == nil {
			log.Printf("[WARN] Schema or schema block is nil for resource type %s", resourceName)
			continue
		}

		currentReadOnly := []string{"^id$"}
		currentReadOnly = p.readBlocksV6Grpc(schema.Block, currentReadOnly, "")
		readOnlyAttributes[resourceName] = currentReadOnly
	}
	return readOnlyAttributes, nil
}

// readBlocksV6Grpc reads blocks from gRPC schema (tfplugin6.Schema_Block)
func (p *ProviderWrapper) readBlocksV6Grpc(block *tfplugin6.Schema_Block, readOnlyAttrs []string, parentPath string) []string {
	if block == nil {
		return readOnlyAttrs
	}
	for _, attr := range block.Attributes {
		if attr.Computed && !attr.Optional && !attr.Required {
			attrPath := attr.Name
			if parentPath != "" {
				attrPath = parentPath + "." + attr.Name
			}
			readOnlyAttrs = append(readOnlyAttrs, "^"+regexpEscape(attrPath)+"$")
		}
	}
	for _, nestedBlock := range block.BlockTypes {
		newParentPath := nestedBlock.TypeName
		if parentPath != "" {
			newParentPath = parentPath + "." + nestedBlock.TypeName
		}
		readOnlyAttrs = p.readBlocksV6Grpc(nestedBlock.Block, readOnlyAttrs, newParentPath)
	}
	return readOnlyAttrs
}

func regexpEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `.`, `\.`)
}

// ctyValueToDynamicValue converts a cty.Value to a tfplugin6.DynamicValue using msgpack
func ctyValueToDynamicValue(val cty.Value, ty cty.Type) (*tfplugin6.DynamicValue, error) {
	if val.IsNull() {
		return &tfplugin6.DynamicValue{}, nil
	}

	// Encode as msgpack
	msgpackBytes, err := msgpack.Marshal(val, ty)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cty.Value to msgpack: %w", err)
	}

	return &tfplugin6.DynamicValue{
		Msgpack: msgpackBytes,
	}, nil
}

// dynamicValueToCtyValue converts a tfplugin6.DynamicValue to a cty.Value
func dynamicValueToCtyValue(dv *tfplugin6.DynamicValue, ty cty.Type) (cty.Value, error) {
	if dv == nil || (len(dv.Msgpack) == 0 && len(dv.Json) == 0) {
		return cty.NullVal(ty), nil
	}

	// Prefer msgpack, fall back to JSON
	if len(dv.Msgpack) > 0 {
		val, err := msgpack.Unmarshal(dv.Msgpack, ty)
		if err != nil {
			return cty.NilVal, fmt.Errorf("failed to unmarshal msgpack to cty.Value: %w", err)
		}
		return val, nil
	}

	if len(dv.Json) > 0 {
		// Use cty's JSON unmarshaling
		val, err := unmarshalJSONToCty(dv.Json, ty)
		if err != nil {
			return cty.NilVal, fmt.Errorf("failed to unmarshal JSON to cty.Value: %w", err)
		}
		return val, nil
	}

	return cty.NullVal(ty), nil
}

// unmarshalJSONToCty is a helper to unmarshal JSON bytes to cty.Value
func unmarshalJSONToCty(data []byte, ty cty.Type) (cty.Value, error) {
	// For dynamic type, we need to infer the type from JSON
	if ty == cty.DynamicPseudoType {
		// Parse as generic interface and convert
		var raw interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			return cty.NilVal, err
		}
		// Convert interface{} to cty.Value (simplified - may need more robust conversion)
		return interfaceToCtyValue(raw), nil
	}

	// For known types, use msgpack library's JSON support if available
	// For now, return an error as we need the schema to properly unmarshal
	return cty.NilVal, fmt.Errorf("JSON unmarshaling with known type not yet implemented")
}

// interfaceToCtyValue converts a generic interface{} from JSON to cty.Value
func interfaceToCtyValue(val interface{}) cty.Value {
	if val == nil {
		return cty.NullVal(cty.DynamicPseudoType)
	}

	switch v := val.(type) {
	case string:
		return cty.StringVal(v)
	case float64:
		return cty.NumberFloatVal(v)
	case bool:
		return cty.BoolVal(v)
	case map[string]interface{}:
		attrs := make(map[string]cty.Value)
		for k, attrVal := range v {
			attrs[k] = interfaceToCtyValue(attrVal)
		}
		return cty.ObjectVal(attrs)
	case []interface{}:
		vals := make([]cty.Value, len(v))
		for i, elemVal := range v {
			vals[i] = interfaceToCtyValue(elemVal)
		}
		return cty.TupleVal(vals)
	default:
		return cty.NullVal(cty.DynamicPseudoType)
	}
}

// Refresh reads the current state of a resource from the provider
func (p *ProviderWrapper) Refresh(infoType string, currentId string, priorStateCty cty.Value, version int64) (cty.Value, error) {
	log.Printf("[DEBUG] ProviderWrapper: Refreshing resource %s with ID %s", infoType, currentId)

	// Get resource schema
	resourceSchema, err := p.GetResourceSchema(context.Background(), infoType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to get resource schema for %s: %w", infoType, err)
	}
	if resourceSchema == nil || resourceSchema.Block == nil {
		return cty.NilVal, fmt.Errorf("nil schema or block for resource type %s", infoType)
	}

	// Prepare gRPC request
	req := &tfplugin6.ReadResource_Request{
		TypeName: infoType,
	}

	// Convert prior state to DynamicValue if provided
	if !priorStateCty.IsNull() && priorStateCty.IsKnown() {
		currentDV, err := ctyValueToDynamicValue(priorStateCty, priorStateCty.Type())
		if err != nil {
			log.Printf("[WARN] Failed to convert priorState for %s: %v. Proceeding without CurrentState.", infoType, err)
		} else {
			req.CurrentState = currentDV
		}
	}

	// Call gRPC ReadResource
	resp, err := p.ProviderClient.ReadResource(context.Background(), req)
	if err != nil {
		return cty.NilVal, fmt.Errorf("ReadResource RPC call failed for %s with ID %s: %w", infoType, currentId, err)
	}

	// Handle diagnostics
	if resp.Diagnostics != nil && len(resp.Diagnostics) > 0 {
		var errs []string
		hasError := false
		for _, diag := range resp.Diagnostics {
			errs = append(errs, fmt.Sprintf("%s: %s", diag.Summary, diag.Detail))
			if diag.Severity == tfplugin6.Diagnostic_ERROR {
				hasError = true
			}
		}
		if hasError {
			log.Printf("[ERROR] ReadResource failed for %s ID %s: %v. Attempting import fallback.", infoType, currentId, errs)
			return p.importResourceStateByID(infoType, currentId, resourceSchema)
		}
		log.Printf("[WARN] ReadResource for %s ID %s returned warnings: %v", infoType, currentId, errs)
	}

	// Check if resource was deleted
	if resp.NewState == nil || (len(resp.NewState.Msgpack) == 0 && len(resp.NewState.Json) == 0) {
		log.Printf("[INFO] ReadResource for %s ID %s returned no state (resource likely deleted)", infoType, currentId)
		return cty.NullVal(cty.DynamicPseudoType), nil
	}

	// Convert response state back to cty.Value
	ctyVal, err := dynamicValueToCtyValue(resp.NewState, cty.DynamicPseudoType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to unmarshal NewState for %s: %w", infoType, err)
	}

	log.Printf("[DEBUG] ProviderWrapper: Successfully refreshed resource %s ID %s", infoType, currentId)
	return ctyVal, nil
}

// importResourceStateByID attempts to import a resource by ID as a fallback
func (p *ProviderWrapper) importResourceStateByID(typeName string, id string, resourceSchema *tfplugin6.Schema) (cty.Value, error) {
	log.Printf("[DEBUG] ProviderWrapper: Attempting ImportResourceState for %s with ID %s", typeName, id)

	// Use the proper ImportResourceState RPC call
	req := &tfplugin6.ImportResourceState_Request{
		TypeName: typeName,
		Id:       id,
	}

	resp, err := p.ProviderClient.ImportResourceState(context.Background(), req)
	if err != nil {
		return cty.NilVal, fmt.Errorf("ImportResourceState RPC failed for %s ID %s: %w", typeName, id, err)
	}

	// Check for errors in diagnostics
	if resp.Diagnostics != nil && len(resp.Diagnostics) > 0 {
		var errs []string
		hasError := false
		for _, diag := range resp.Diagnostics {
			errs = append(errs, fmt.Sprintf("%s: %s", diag.Summary, diag.Detail))
			if diag.Severity == tfplugin6.Diagnostic_ERROR {
				hasError = true
			}
		}
		if hasError {
			return cty.NilVal, fmt.Errorf("import for %s ID %s failed: %v", typeName, id, errs)
		}
		log.Printf("[WARN] Import for %s ID %s returned warnings: %v", typeName, id, errs)
	}

	// ImportResourceState can return multiple imported resources (for cases like AWS security group rules)
	// For now, just take the first one
	if resp.ImportedResources == nil || len(resp.ImportedResources) == 0 {
		return cty.NilVal, fmt.Errorf("import for %s ID %s returned no resources", typeName, id)
	}

	importedResource := resp.ImportedResources[0]
	if importedResource.State == nil || (len(importedResource.State.Msgpack) == 0 && len(importedResource.State.Json) == 0) {
		return cty.NilVal, fmt.Errorf("import for %s ID %s returned empty state", typeName, id)
	}

	// Convert response to cty.Value
	ctyVal, err := dynamicValueToCtyValue(importedResource.State, cty.DynamicPseudoType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to unmarshal state from import for %s: %w", typeName, err)
	}

	log.Printf("[DEBUG] ProviderWrapper: Successfully imported resource %s ID %s", typeName, id)
	return ctyVal, nil
}

func (p *ProviderWrapper) initProvider(verbose bool) error {
	providerFilePath, err := getProviderFileName(p.providerName)
	if err != nil {
		return fmt.Errorf("failed to get provider_old filename for %s: %w", p.providerName, err)
	}
	log.Printf("[DEBUG] ProviderWrapper: Found provider_old executable at %s", providerFilePath)

	options := hclog.LoggerOptions{
		Name:   "plugin",
		Level:  hclog.Error,
		Output: os.Stdout,
	}
	if verbose {
		options.Level = hclog.Trace
		log.Println("[DEBUG] ProviderWrapper: Verbose logging enabled for plugin.")
	}
	logger := hclog.New(&options)

	p.client = plugin.NewClient(
		&plugin.ClientConfig{
			Cmd:              exec.Command(providerFilePath),
			HandshakeConfig:  Handshake,
			VersionedPlugins: VersionedPlugins,
			Managed:          true,
			Logger:           logger,
			AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
			AutoMTLS:         true,
		})

	p.rpcClient, err = p.client.Client()
	if err != nil {
		return fmt.Errorf("failed to get plugin client: %w", err)
	}

	raw, err := p.rpcClient.Dispense("provider")
	if err != nil {
		return fmt.Errorf("failed to dispense provider plugin: %w", err)
	}

	var ok bool
	p.ProviderClient, ok = raw.(tfplugin6.ProviderClient)
	if !ok {
		return fmt.Errorf("dispensed plugin is not a tfplugin6.ProviderClient; got %T", raw)
	}
	log.Println("[DEBUG] ProviderWrapper: Successfully dispensed tfplugin6.ProviderClient.")

	// Get schema to validate provider is working
	schemaResp, err := p.ProviderClient.GetProviderSchema(context.Background(), &tfplugin6.GetProviderSchema_Request{})
	if err != nil {
		return fmt.Errorf("failed to get provider schema for configuration: %w", err)
	}
	if schemaResp.Provider == nil || schemaResp.Provider.Block == nil {
		return fmt.Errorf("provider schema or schema block is nil during configuration")
	}

	// Convert config to DynamicValue
	var configDynamicValue *tfplugin6.DynamicValue
	if !p.config.IsNull() && p.config.IsKnown() {
		configDynamicValue, err = ctyValueToDynamicValue(p.config, p.config.Type())
		if err != nil {
			return fmt.Errorf("failed to convert provider config cty.Value to DynamicValue: %w", err)
		}
	} else {
		configDynamicValue = &tfplugin6.DynamicValue{}
		log.Println("[DEBUG] ProviderWrapper: Provider config is null or unknown, using empty DynamicValue.")
	}

	// Configure the provider
	configureReq := &tfplugin6.ConfigureProvider_Request{
		TerraformVersion: "1.5.0",
		Config:           configDynamicValue,
	}

	log.Println("[DEBUG] ProviderWrapper: Configuring provider...")
	configureResp, err := p.ProviderClient.ConfigureProvider(context.Background(), configureReq)
	if err != nil {
		p.client.Kill()
		return fmt.Errorf("failed to configure provider: %w", err)
	}

	if configureResp.Diagnostics != nil && len(configureResp.Diagnostics) > 0 {
		for _, diag := range configureResp.Diagnostics {
			log.Printf("[%s] ProviderWrapper: Diagnostic from ConfigureProvider: %s: %s\n", diag.Severity, diag.Summary, diag.Detail)
			if diag.Severity == tfplugin6.Diagnostic_ERROR {
				p.client.Kill()
				return fmt.Errorf("error configuring provider: %s - %s", diag.Summary, diag.Detail)
			}
		}
	}
	log.Println("[DEBUG] ProviderWrapper: Provider configured successfully.")
	return nil
}

func getProviderFileName(providerName string) (string, error) {
	defaultDataDir := os.Getenv("TF_DATA_DIR")
	if defaultDataDir == "" {
		defaultDataDir = DefaultDataDir
	}
	providerFilePath, err := getProviderFileNameV13andV14(defaultDataDir, providerName)
	if err != nil || providerFilePath == "" {
		providerFilePath, err = getProviderFileNameV13andV14(os.Getenv("HOME")+string(os.PathSeparator)+
			".terraform.d", providerName)
	}
	if err != nil || providerFilePath == "" {
		return getProviderFileNameV12(providerName)
	}
	return providerFilePath, nil
}

func getProviderFileNameV13andV14(prefix, providerName string) (string, error) {
	registryDir := prefix + string(os.PathSeparator) + "providers" + string(os.PathSeparator) +
		"registry.terraform.io"
	providerDirs, err := os.ReadDir(registryDir)
	if err != nil {
		registryDir = prefix + string(os.PathSeparator) + "plugins" + string(os.PathSeparator) +
			"registry.terraform.io"
		providerDirs, err = os.ReadDir(registryDir)
		if err != nil {
			return "", err
		}
	}
	providerFilePath := ""
	for _, providerDir := range providerDirs {
		pluginPath := registryDir + string(os.PathSeparator) + providerDir.Name() +
			string(os.PathSeparator) + providerName
		dirs, err := os.ReadDir(pluginPath)
		if err != nil {
			continue
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			versionedDirs, err := os.ReadDir(pluginPath + string(os.PathSeparator) + dir.Name())
			if err != nil {
				continue
			}
			for _, versionDir := range versionedDirs {
				if !versionDir.IsDir() {
					continue
				}
				fullPluginPath := pluginPath + string(os.PathSeparator) + dir.Name() +
					string(os.PathSeparator) + versionDir.Name() +
					string(os.PathSeparator) + runtime.GOOS + "_" + runtime.GOARCH
				files, err := os.ReadDir(fullPluginPath)
				if err == nil {
					for _, file := range files {
						if strings.HasPrefix(file.Name(), "terraform-provider-"+providerName) {
							providerFilePath = fullPluginPath + string(os.PathSeparator) + file.Name()
							return providerFilePath, nil
						}
					}
				}
			}
		}
	}
	if providerFilePath == "" {
	}
	return providerFilePath, nil
}

func getProviderFileNameV12(providerName string) (string, error) {
	defaultDataDir := os.Getenv("TF_DATA_DIR")
	if defaultDataDir == "" {
		defaultDataDir = DefaultDataDir
	}
	pluginPath := defaultDataDir + string(os.PathSeparator) + "plugins" + string(os.PathSeparator) + runtime.GOOS + "_" + runtime.GOARCH
	files, err := os.ReadDir(pluginPath)
	if err != nil {
		homePluginDir := ""
		homeDir, homeErr := os.UserHomeDir()
		if homeErr == nil {
			homePluginDir = homeDir + string(os.PathSeparator) + "." + DefaultPluginVendorDirV12
		}

		if homePluginDir != "" {
			pluginPath = homePluginDir
			files, err = os.ReadDir(pluginPath)
		}
		if err != nil {
			return "", err
		}
	}
	providerFilePath := ""
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if strings.HasPrefix(file.Name(), "terraform-provider-"+providerName) {
			providerFilePath = pluginPath + string(os.PathSeparator) + file.Name()
			return providerFilePath, nil
		}
	}
	return "", fmt.Errorf("v12 provider_old binary not found for %s in %s or home dir", providerName, pluginPath)
}

func GetProviderVersion(providerName string) string {
	providerFilePath, err := getProviderFileName(providerName)
	if err != nil {
		log.Println("Can't find provider_old file path. Ensure that you are following https://www.terraform.io/docs/configuration/providers.html#third-party-plugins.")
		return ""
	}
	t := strings.Split(providerFilePath, string(os.PathSeparator))
	providerFileName := t[len(t)-1]
	parts := strings.Split(providerFileName, "_")
	if len(parts) >= 2 {
		versionPart := parts[1]
		if strings.HasPrefix(versionPart, "v") {
			return "~> " + strings.TrimPrefix(versionPart, "v")
		}
	}
	log.Println("Can't find provider_old version from filename. Ensure plugin naming convention terraform-provider-NAME_vX.Y.Z.")
	return ""
}
