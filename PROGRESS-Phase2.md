# Phase 2: State Management Migration

**Goal**: Migrate resource.go and utils.go from old Terraform v0.12 state types to work with the new provider wrapper

**Starting Point**: Commit `36628561` - Provider wrapper compiles successfully!

---

## Overview

Phase 1 successfully migrated the provider wrapper to use terraform-plugin-sdk/v2 with gRPC types. However, the rest of the codebase still uses old Terraform v0.12 types that no longer exist:

**Old Types (Need to Replace)**:
- `terraform.InstanceInfo` - Resource metadata (type, ID)
- `terraform.InstanceState` - Resource state with attributes map
- `terraform.OutputState` - Terraform outputs
- `terraform.WriteState()` - State file writing

**Problem**: These types were removed in Terraform v1.0+ and don't exist in the new SDK.

---

## Current Compilation Errors

```
terraformutils/resource.go:25: no required module provides package github.com/hashicorp/terraform/terraform
```

**Files Affected**:
- `terraformutils/resource.go` - Uses terraform.InstanceInfo, terraform.InstanceState
- `terraformutils/utils.go` - Uses terraform.WriteState, terraform.State
- `terraformutils/terraformoutput/hcl.go` - Writes state files
- `terraformutils/service_test.go` - Test fixtures
- `providers/alicloud/connectivity/client.go` - Imports (may be unused)

---

## Strategy: Lightweight Internal Types

Instead of migrating to Terraform State v4 JSON format (which is complex), we'll create simple internal types that match our current usage:

### Option A: Keep Map-Based State (RECOMMENDED)
**Pros**: Minimal changes, matches current usage
**Cons**: Not "proper" Terraform state format

```go
// Simple internal types that match current usage
type InstanceInfo struct {
    Type string  // Resource type (e.g., "aws_instance")
    Id   string  // Resource identifier
}

type InstanceState struct {
    ID         string
    Attributes map[string]string  // Flat key-value map
}

type OutputState struct {
    Value interface{}
    Type  string
}
```

### Option B: Use cty.Value Everywhere
**Pros**: More aligned with new Terraform architecture
**Cons**: More refactoring needed, changes many call sites

---

## Implementation Plan

### Step 1: Create Internal State Types
**File**: `terraformutils/state_types.go` (new file)

Create drop-in replacements for:
- InstanceInfo
- InstanceState
- OutputState

These will be simple structs that preserve the current API.

### Step 2: Update resource.go
**Changes**:
1. Replace `terraform.InstanceInfo` → `terraformutils.InstanceInfo`
2. Replace `terraform.InstanceState` → `terraformutils.InstanceState`
3. Replace `terraform.OutputState` → `terraformutils.OutputState`
4. Update imports

**Estimated**: ~10 line changes

### Step 3: Update utils.go
**Changes**:
1. Replace state writing logic
2. Remove `terraform.WriteState()` calls
3. May need custom state file writing (or skip if not needed)

**Estimated**: ~20 line changes

### Step 4: Update terraformoutput/hcl.go
**Changes**:
1. Update state serialization if needed
2. May be able to keep as-is if it only uses HCL generation

**Estimated**: ~5-10 line changes

### Step 5: Clean Up Tests
**Changes**:
1. Update test fixtures in service_test.go
2. Update provider_test.go schema definitions

**Estimated**: ~15 line changes

---

## Key Decisions

### Q1: Do we need full Terraform State v4 format?
**Answer**: NO - Terraformer generates .tf files and tfstate, but the internal representation can be simpler.

### Q2: Should we use JSON state format?
**Answer**: Not yet - keep map[string]string for now, can enhance later if needed.

### Q3: What about state file writing?
**Answer**: Check if WriteState() is actually used. If yes, implement simple JSON serialization.

---

## Expected Outcome

After Phase 2:
- ✅ All `terraform.*` imports removed
- ✅ `terraformutils` package compiles
- ✅ No dependency on old Terraform SDK
- ✅ Ready for Phase 3 (fix remaining providers)

**Estimated Effort**: 1-2 hours of careful refactoring

---

## Risks & Mitigation

**Risk 1**: Breaking existing functionality
- **Mitigation**: Keep the same API surface, just change implementation
- **Test**: Run existing tests after each change

**Risk 2**: State format incompatibility
- **Mitigation**: Start with exact same format (map[string]string)
- **Fallback**: Can always revert to commit `36628561`

**Risk 3**: Unknown dependencies on terraform.* types
- **Mitigation**: Compile frequently, fix errors as they appear
- **Search**: Grep for "terraform\." to find all usages

---

## Next Steps

1. Create `terraformutils/state_types.go` with internal types
2. Update `resource.go` imports and types
3. Update `utils.go` to use new types
4. Fix any compilation errors
5. Test with a simple provider import
6. Commit progress

---

## Files to Create/Modify

**New Files**:
- `terraformutils/state_types.go` - Internal state type definitions

**Files to Modify**:
- `terraformutils/resource.go` - Replace terraform.* types
- `terraformutils/utils.go` - Replace state writing
- `terraformutils/terraformoutput/hcl.go` - Update if needed
- `terraformutils/service_test.go` - Update test fixtures
- `providers/alicloud/connectivity/client.go` - Remove unused import

**Estimated Total Changes**: ~60 lines across 6 files
