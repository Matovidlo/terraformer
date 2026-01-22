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
	"fmt"
	"log"
	"net/rpc"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/fromproto"
	"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6"
	"github.com/hashicorp/go-cty/cty"
	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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
	ProviderClient tfplugin6.ProviderClient
	client         *plugin.Client
	rpcClient      plugin.ClientProtocol
	providerName   string
	config         cty.Value
	schemaV6       *tfprotov6.Schema
	retryCount     int
	retrySleepMs   int
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

func (p *ProviderWrapper) GetSchema() *tfprotov6.Schema {
	if p.schemaV6 == nil {
		log.Println("[DEBUG] ProviderWrapper: GetSchema called, schemaV6 is nil. Fetching from provider.")
		if p.ProviderClient == nil {
			log.Println("[ERROR] ProviderWrapper: ProviderClient is nil in GetSchema")
			return nil
		}
		// Call GetProviderSchema from tfplugin6 (gRPC) and convert to tfprotov6
		grpcResp, err := p.ProviderClient.GetProviderSchema(context.Background(), &tfplugin6.GetProviderSchema_Request{})
		if err != nil {
			log.Printf("[ERROR] ProviderWrapper: GetProviderSchema RPC call failed: %v\n", err)
			return nil
		}
		if grpcResp.Diagnostics != nil && len(grpcResp.Diagnostics) > 0 {
			for _, diag := range grpcResp.Diagnostics {
				log.Printf("[ERROR] ProviderWrapper: Diagnostics from GetProviderSchema: %s: %s\n", diag.Summary, diag.Detail)
			}
		}
		// TODO: Convert from tfplugin6.Schema to tfprotov6.Schema
		// For now, store the gRPC schema and adapt later
		log.Printf("[DEBUG] ProviderWrapper: GetProviderSchema successful. Provider schema: %+v\n", grpcResp.Provider)
		// We need to convert grpcResp.Provider (tfplugin6 schema) to tfprotov6.Schema
		// This requires using the fromproto package which is also internal
		// For now, we'll need to work with tfplugin6 types directly
		p.schemaV6 = nil // TODO: Fix this conversion
	}
	return p.schemaV6
}

func (p *ProviderWrapper) GetResourceSchema(ctx context.Context, typeName string) (*tfprotov6.Schema, error) {
	resp, err := p.ProviderClient.GetSchema(ctx, &tfprotov6.GetSchemaRequest{})
	if err != nil {
		return nil, fmt.Errorf("get schema rpc: %w", err)
	}
	rs, ok := resp.ResourceSchemas[typeName]
	if !ok {
		return nil, fmt.Errorf("resource type %q not found in provider schema", typeName)
	}
	return rs, nil
}

func (p *ProviderWrapper) GetReadOnlyAttributes(resourceTypes []string) (map[string][]string, error) {
	log.Println("TODO: Rewrite GetReadOnlyAttributes for tfprotov6.Schema")
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
		currentReadOnly = p.readBlocksV6(schema.Block, currentReadOnly, "")
		readOnlyAttributes[resourceName] = currentReadOnly
	}
	return readOnlyAttributes, nil
}

func (p *ProviderWrapper) readBlocksV6(block *tfprotov6.Block, readOnlyAttrs []string, parentPath string) []string {
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
		readOnlyAttrs = p.readBlocksV6(nestedBlock.Block, readOnlyAttrs, newParentPath)
	}
	return readOnlyAttrs
}

func regexpEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `.`, `\.`)
}

func (p *ProviderWrapper) Refresh(infoType string, currentId string, priorStateCty cty.Value, version int64) (cty.Value, error) {
	log.Println("TODO: Rewrite Refresh for tfprotov6")

	req := &tfprotov6.ReadResourceRequest{
		TypeName: infoType,
	}

	resourceSchema, err := p.GetResourceSchema(context.Background(), infoType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to get resource schema for %s: %w", infoType, err)
	}
	if resourceSchema == nil || resourceSchema.Block == nil {
		return cty.NilVal, fmt.Errorf("nil schema or block for resource type %s", infoType)
	}

	var currentDynamicValue *tfprotov6.DynamicValue
	if !priorStateCty.IsNull() && priorStateCty.IsKnown() {
		if priorStateCty.Type().IsObjectType() {
			currentDynamicValue, err = tfprotov6.NewDynamicValue(priorStateCty.Type(), priorStateCty)
			if err != nil {
				log.Printf("[WARN] Failed to convert priorState cty.Value to DynamicValue for %s: %v. Proceeding with nil CurrentState.", infoType, err)
			} else {
				req.CurrentState = currentDynamicValue
			}
		} else {
			log.Printf("[WARN] priorStateCty is not an object type for %s. Proceeding with nil CurrentState.", infoType)
		}
	}

	resp, err := p.ProviderClient.ReadResource(context.Background(), req)
	if err != nil {
		return cty.NilVal, fmt.Errorf("ReadResource RPC call failed for %s with ID %s: %w", infoType, currentId, err)
	}

	if resp.Diagnostics != nil && len(resp.Diagnostics) > 0 {
		var errs []string
		for _, diag := range resp.Diagnostics {
			errs = append(errs, fmt.Sprintf("%s: %s", diag.Summary, diag.Detail))
			if diag.Severity == tfprotov6.DiagnosticSeverityError {
				log.Printf("ReadResource failed for %s ID %s, attempting import: %s", infoType, currentId, diag.Summary)
				return p.importResourceStateByID(infoType, currentId, resourceSchema)
			}
		}
		if resp.NewState == nil || resp.NewState.Msgpack == nil {
			log.Printf("ReadResource for %s ID %s returned diagnostics and null/empty NewState. Assuming resource is gone or unreadable. Diagnostics: %s", infoType, currentId, strings.Join(errs, "; "))
			return cty.NilVal, fmt.Errorf("resource %s ID %s not found or unreadable after Read attempt. Diagnostics: %s", infoType, currentId, strings.Join(errs, "; "))
		}
		log.Printf("[WARN] ReadResource for %s ID %s returned diagnostics: %s", infoType, currentId, strings.Join(errs, "; "))
	}

	if resp.NewState == nil || resp.NewState.Msgpack == nil {
		log.Printf("ReadResource for %s ID %s returned no state (resource likely deleted or doesn't exist)", infoType, currentId)
		return cty.NilVal, nil
	}

	ctyVal, err := resp.NewState.UnmarshalToCTYValue(cty.DynamicPseudoType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to unmarshal NewState DynamicValue to cty.Value for %s: %w", infoType, err)
	}
	if !ctyVal.Type().IsObjectType() {
		log.Printf("[WARN] ReadResource for %s ID %s NewState unmarshalled to non-object cty.Type: %s. This might be an issue.", infoType, currentId, ctyVal.Type().FriendlyName())
	}

	return ctyVal, nil
}

func (p *ProviderWrapper) importResourceStateByID(typeName string, id string, resourceSchema *tfprotov6.Schema) (cty.Value, error) {
	log.Printf("Attempting ImportResourceState for %s with ID %s", typeName, id)

	idVal := cty.StringVal(id)
	idAttrType := tftypes.String
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"id": idAttrType}}

	idOnlyStateVal := cty.ObjectVal(map[string]cty.Value{"id": idVal})

	dynamicIDState, err := tfprotov6.NewDynamicValue(objType, idOnlyStateVal)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to create DynamicValue for ID-only state for %s: %w", typeName, err)
	}

	importReadReq := &tfprotov6.ReadResourceRequest{
		TypeName:     typeName,
		CurrentState: dynamicIDState,
	}

	resp, err := p.ProviderClient.ReadResource(context.Background(), importReadReq)
	if err != nil {
		return cty.NilVal, fmt.Errorf("fallback ReadResource (import) RPC call failed for %s ID %s: %w", typeName, id, err)
	}
	if resp.Diagnostics != nil && len(resp.Diagnostics) > 0 {
		var errs []string
		for _, diag := range resp.Diagnostics {
			errs = append(errs, fmt.Sprintf("%s: %s", diag.Summary, diag.Detail))
			if diag.Severity == tfprotov6.DiagnosticSeverityError {
				return cty.NilVal, fmt.Errorf("fallback ReadResource (import) for %s ID %s failed with errors: %s", typeName, id, strings.Join(errs, "; "))
			}
		}
		log.Printf("[WARN] Fallback ReadResource (import) for %s ID %s returned diagnostics: %s", typeName, id, strings.Join(errs, "; "))
	}
	if resp.NewState == nil || resp.NewState.Msgpack == nil {
		return cty.NilVal, fmt.Errorf("fallback ReadResource (import) for %s ID %s returned no state", typeName, id)
	}

	ctyVal, err := resp.NewState.UnmarshalToCTYValue(cty.DynamicPseudoType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to unmarshal NewState from fallback import for %s: %w", typeName, err)
	}
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
			HandshakeConfig:  tfplugin6.Handshake,
			VersionedPlugins: tfplugin6.VersionedPlugins,
			Managed:          true,
			Logger:           logger,
			AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
			AutoMTLS:         true,
		})

	p.rpcClient, err = p.client.Client()
	if err != nil {
		return fmt.Errorf("failed to get plugin client: %w", err)
	}

	raw, err := p.rpcClient.Dispense(tfprotov6.ProviderPluginName)
	if err != nil {
		return fmt.Errorf("failed to dispense provider_old plugin: %w", err)
	}

	var ok bool
	p.ProviderClient, ok = raw.(tfplugin6.ProviderClient)
	if !ok {
		return fmt.Errorf("dispensed plugin is not a tfplugin6.ProviderClient; got %T", raw)
	}
	log.Println("[DEBUG] ProviderWrapper: Successfully dispensed tfplugin6.ProviderClient.")

	schemaResp, err := p.ProviderClient.GetSchema(context.Background(), &tfprotov6.GetSchemaRequest{})
	if err != nil {
		return fmt.Errorf("failed to get provider_old schema for configuration: %w", err)
	}
	if schemaResp.Provider == nil || schemaResp.Provider.Block == nil {
		return fmt.Errorf("provider_old schema or schema block is nil during configuration")
	}

	var configDynamicValue *tfprotov6.DynamicValue
	if !p.config.IsNull() && p.config.IsKnown() {
		configDynamicValue, err = tfprotov6.NewDynamicValue(p.config.Type(), p.config)
		if err != nil {
			return fmt.Errorf("failed to convert provider_old config cty.Value to DynamicValue: %w", err)
		}
	} else {
		emptyObjectType := tftypes.Object{}
		configDynamicValue, _ = tfprotov6.NewDynamicValue(emptyObjectType, cty.NullVal(emptyObjectType))

		log.Println("[DEBUG] ProviderWrapper: Provider config is null or unknown, using a null DynamicValue for configuration.")
	}

	configureReq := &tfprotov6.ConfigureProviderRequest{
		TerraformVersion: "0.15.0",
		Config:           configDynamicValue,
	}

	log.Println("[DEBUG] ProviderWrapper: Configuring provider_old...")
	configureResp, err := p.ProviderClient.Configure(context.Background(), configureReq)
	if err != nil {
		p.client.Kill()
		return fmt.Errorf("failed to configure provider_old: %w", err)
	}

	if configureResp.Diagnostics != nil && len(configureResp.Diagnostics) > 0 {
		for _, diag := range configureResp.Diagnostics {
			log.Printf("[%s] ProviderWrapper: Diagnostic from ConfigureProvider: %s: %s\n", diag.Severity, diag.Summary, diag.Detail)
			if diag.Severity == tfprotov6.DiagnosticSeverityError {
				p.client.Kill()
				return fmt.Errorf("error configuring provider_old: %s - %s", diag.Summary, diag.Detail)
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
