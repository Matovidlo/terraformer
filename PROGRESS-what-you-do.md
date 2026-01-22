# Terraform SDK Migration Progress Log

**Goal**: Migrate from Terraform SDK v0.12.31 to terraform-plugin-sdk/v2 v2.29.0

**Checkpoint Commit**: `8ddddcf7` - WIP: Partial Terraform SDK migration v0.12 to v2.29.0

---

## Phase 1: Fix Provider Wrapper (CURRENT - IN PROGRESS)

### Problem Identified
The provider wrapper was trying to import `github.com/hashicorp/terraform-plugin-go/tfprotov6/tfplugin6` which doesn't exist publicly - it's in an internal package at `github.com/hashicorp/terraform-plugin-go/tfprotov6/internal/tfplugin6`.

Go compiler won't allow importing internal packages from external modules, so we needed an alternative approach.

### Solution: Vendor the tfplugin6 Package

Based on the header comment in the tfplugin6 generated files which explicitly states:
> "To implement a plugin against this protocol, copy this definition into your own codebase and use protoc to generate stubs for your target language."

HashiCorp intends for consumers to vendor/copy this code.

### Steps Taken

1. **Created vendor directory structure**:
   ```bash
   mkdir -p terraformutils/providerwrapper/internal/tfplugin6
   ```

2. **Copied tfplugin6 files from go modules** (from `~/go/pkg/mod/github.com/hashicorp/terraform-plugin-go@v0.29.0/tfprotov6/internal/tfplugin6/`):
   ```bash
   cp tfplugin6.pb.go terraformutils/providerwrapper/internal/tfplugin6/
   cp tfplugin6_grpc.pb.go terraformutils/providerwrapper/internal/tfplugin6/
   cp tfplugin6.proto terraformutils/providerwrapper/internal/tfplugin6/
   ```

3. **Updated import in `terraformutils/providerwrapper/provider.go`**:
   - Changed from: `"github.com/hashicorp/terraform-plugin-go/tfprotov6/internal/tfplugin6"`
   - To: `"github.com/GoogleCloudPlatform/terraformer/terraformutils/providerwrapper/internal/tfplugin6"`

4. **Added necessary plugin configuration** in `provider.go`:
   - Defined `Handshake` config with protocol version 6 and magic cookie
   - Implemented `GRPCProviderPlugin` struct with client-side gRPC plugin interface
   - Created `VersionedPlugins` map for protocol negotiation

### Modified Files So Far
- `terraformutils/providerwrapper/provider.go` - Updated imports and added plugin config
- `terraformutils/providerwrapper/internal/tfplugin6/` - New vendored package (3 files)

### Next Steps
1. Test if provider wrapper compiles now
2. Update method signatures to use proper gRPC types
3. Handle conversion between tfplugin6 (gRPC) and tfprotov6 (public API) types

---

## Phase 2: State Management Migration (PENDING)

Will need to:
- Create `terraformutils/state/state_v4.go` with Terraform State v4 JSON format
- Migrate `resource.go`, `utils.go`, `terraformoutput/hcl.go` to use new state format
- Replace `terraform.InstanceState`, `terraform.InstanceInfo`, etc. with new structures

---

## Phase 3: Schema and Testing Updates (PENDING)

Will need to:
- Migrate `provider_test.go` from `configschema` to SDK v2 schema types
- Fix `service_test.go` test fixtures
- Clean up unused terraform imports in `providers/alicloud/connectivity/client.go`

---

## Phase 4: Dependency Resolution (PENDING)

- Run `go mod tidy` to clean up dependencies
- Ensure no imports from old `github.com/hashicorp/terraform` packages remain

---

## Phase 5: Build and Test (PENDING)

- Fix all compilation errors
- Run test suite
- Test actual provider import functionality

---

## Key Decisions Made

1. **Vendoring tfplugin6**: Decided to copy the internal tfplugin6 package into our codebase rather than trying to use internal imports, as recommended by HashiCorp's proto file header comments.

2. **Protocol Version**: Using protocol v6 for compatibility with modern Terraform providers.

3. **Client-Only Implementation**: Only implementing gRPC client side (not server) since Terraformer consumes providers, it doesn't implement them.

---

## Reference Links

- [Terraform Plugin Protocol v6](https://github.com/hashicorp/terraform-plugin-go/tree/main/tfprotov6)
- [terraform-plugin-sdk/v2 Docs](https://pkg.go.dev/github.com/hashicorp/terraform-plugin-sdk/v2)
- [go-plugin Documentation](https://github.com/hashicorp/go-plugin)
