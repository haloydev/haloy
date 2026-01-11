# Server Upgrade Feature Fixes - Implementation Plan

## Status: APPROVED - Ready to Implement

## Overview
Fix high and medium priority issues identified in the server upgrade feature code review.

## Tasks

### 1. Fix SSH Host Extraction (High Priority)
**File**: `internal/haloy/server_upgrade.go`

Add helper function to extract hostname from server URL (strips protocol and port):
- Add `net` and `helpers` imports
- Add `extractSSHHost()` function
- Use it in `performSSHUpgrade()` when building SSH config

### 2. Fix Upgrade Script (High Priority)
**File**: `scripts/upgrade-server.sh`

- Remove haloyd binary download/update logic (lines 61-81)
- Remove manual docker restart logic (lines 89-101)
- Replace with `haloyadm restart --no-logs` call
- This handles both haloyd AND HAProxy version updates automatically

### 3. Remove Unused Token Variable (Medium Priority)
**File**: `internal/haloy/server_upgrade.go`

- Remove token retrieval (lines 72-75)
- Remove token parameter from `performSSHUpgrade()` function
- Update call site

### 4. Keep curl|sh Pattern (Medium Priority)
**Status**: COMPLETED - No changes needed
Consistent with existing pattern in `server_setup.go`

### 5. Add Rollback Mechanism (Medium Priority)
**File**: `scripts/upgrade-server.sh`

- Add trap-based rollback on failure
- Restore haloyadm backup if upgrade fails
- Run `haloyadm restart` to restore services
- Clean up backup on success

## Approved By
User approved implementation on this date.
