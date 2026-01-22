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

### Current Challenge: Type Conversion Complexity

After vendoring tfplugin6, we hit a major issue: the provider wrapper uses **two different type systems**:
1. **tfplugin6** types (gRPC protocol buffers - what we get from provider plugins)
2. **tfprotov6** types (public Go API - what the rest of terraformer uses)

These types need conversion between them. HashiCorp provides conversion in `tfprotov6/internal/fromproto`, but that's also internal.

**Compilation Errors** (10+ errors):
- Type mismatches: `tfplugin6.ProviderClient` vs `tfprotov6` types
- Missing methods: `GetSchema` vs `GetProviderSchema`
- `cty.Value` vs `tftypes.Value` conversion issues
- `tfplugin6.DynamicValue` vs `tfprotov6.DynamicValue` mismatches

### Options Going Forward

**Option A: Vendor fromproto Package Too** (Complex but complete)
- Copy all files from `tfprotov6/internal/fromproto/` (13 non-test files)
- Get full conversion capability between gRPC and public API
- Most correct approach, but ~2000+ lines of code to vendor
- Risk: May have dependencies on other internal packages

**Option B: Write Minimal Conversion Helpers** (Moderate complexity)
- Write only the specific conversions we need
- Smaller code footprint
- Risk: May miss edge cases that HashiCorp's code handles

**Option C: Rework to Use tfplugin6 Throughout** (Major refactor)
- Stop using tfprotov6 types entirely
- Work directly with gRPC types in provider wrapper
- Would require changing multiple files that use ProviderWrapper
- Affects: resource.go, utils.go, service.go, and 6 other files

**Option D: Use terraform-exec Instead** (Different approach)
- Instead of loading providers directly, use terraform-exec library
- Call `terraform import` commands
- Simpler but different architecture

### Recommendation
I recommend **Option A** (vendor fromproto) because:
1. It's what HashiCorp intends (they say "copy this into your codebase")
2. Least risk of bugs from incorrect conversions
3. Preserves existing architecture
4. One-time effort

### Implementation Progress

**Completed** ✅:
1. Vendored tfplugin6 package (3 files)
2. Created plugin handshake configuration
3. Implemented GRPCProviderPlugin for client-side gRPC
4. Vendored fromproto package (12 files) for type conversions
5. Updated import paths in vendored files to use our internal packages

**Current Blockers** ⚠️:
The provider wrapper has type system conflicts:
1. **API Version Mismatch**: Code uses old `terraform.InstanceState`/`terraform.InstanceInfo` but those don't exist in new SDK
2. **Value Type Conflicts**: Code uses `cty.Value` but new API uses `tftypes.Value`
3. **Method Signature Changes**: `GetSchema()` → `GetProviderSchema()`, `UnmarshalToCTYValue()` removed
4. **Conversion Complexity**: Need to convert between:
   - `cty.Value` ↔ `tftypes.Value`
   - `terraform.InstanceState` ↔ `tfprotov6` state types
   - Old request/response types ↔ new gRPC types

**Compilation Errors** (10+):
- Missing fromproto response converters (only request converters exist)
- cty.Value can't convert to DynamicValue (needs tftypes.Value)
- Diagnostic severity type mismatches
- Block/Attribute type incompatibilities

### Next Steps

Need to decide between:

**A. Continue Current Approach** (More work, cleaner result)
- Write cty ↔ tftypes conversion helpers
- Update all provider wrapper methods to use new types
- May uncover more issues as we go
- Estimated: 20-30 more changes needed

**B. Hybrid Approach** (Faster, more technical debt)
- Keep using protobuf types internally in provider wrapper
- Only convert at the boundary where terraformer uses it
- Requires changing ProviderWrapper struct to use tfplugin6 types
- Estimated: 10-15 changes needed

**C. Parallel Rewrite** (Clean slate, most time)
- Create new ProviderWrapperV2 alongside old one
- Migrate consumers one by one
- Can test both in parallel
- Estimated: Full rewrite

**Checkpoint**: Commit `fb9a3d76` - Vendored tfplugin6 and fromproto, facing type system conflicts

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
