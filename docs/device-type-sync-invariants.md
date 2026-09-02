# Device type sync: two invariants

## Scope

Applies to `pkg/controller/sync_device_types.go`, specifically `ensureSyncedService` and
`prepareContentVariable` — the path that grows a device type as uplinks arrive, one event
service per fPort. Not about `SyncDeviceProfile`, which builds a device type from a
ChirpStack device profile and writes it through the device repository client; that path
does not read from the iot cache and does not compare against a previous content variable.

Both invariants were violated at once and produced the same symptom, so a change that
touches this path should check both.

## 1. Never modify what the iot cache returned

`HandleEvent` reads the device type through `connector.IotCache.GetDeviceType`. That cache
returns its stored value without copying — the L1 layer keeps Go values and hands them back
by type assertion. Passing the `models.DeviceType` by value does not help: the copy shares
the `Services` backing array with the cached entry, so `dt.Services[i] = x` writes into the
cache.

The consequence is not limited to the current request. A half-built service written into the
slice stays visible for the rest of the cache TTL, because a failed update does not refresh
the cache. The next event for a different fPort then appends its own service, sends the whole
device type, and the device repository refuses all of it:

```
400 missing content variable name
```

The error names neither the service nor the field, and the offending service is not the one
the request was about.

So: clone before mutating.

```go
dt.Services = slices.Clone(dt.Services)
```

A shallow clone is the right depth here, because only whole elements are replaced. Anything
that writes one level deeper — into an element's `Outputs`, say — clones that level too.

Related: build a replacement service completely before assigning it. An assignment placed
before the content variable exists leaves an inconsistent object behind on every early
return, which is how the above happened.

## 2. Keep the sub content variable order deterministic

`ensureSyncedService` decides whether an update is needed by comparing the prepared content
variable against the stored one with `reflect.DeepEqual`. That comparison is order sensitive,
while `prepareContentVariable` collects sub content variables by iterating Go maps — both the
decoded payload and the `remainingBaseSubs` bookkeeping.

Without sorting, the order differs per call, every event is reported as a change, and the
device type is rewritten on every incoming message with its variables reshuffled each time.
It also makes the first invariant's damage near certain to surface, because it guarantees the
follow-up update that ships the corrupted sibling.

`sortSubContentVariables` runs at the end of `prepareContentVariable` and uses two orders:

- **Structures** sort lexicographically by name.
- **Lists** sort numerically by the index carried in the name. Lexicographic order would put
  `"10"` before `"2"`; in a list the name *is* the position, not just a label.

Two things this does not break:

- `deviceTypeNeedsUpdate`, used by the profile sync, compares content variables by name and
  type and is therefore order independent. It will not write the old order back, so there is
  no ping-pong between the two paths.
- Existing device types are stored unsorted. The first event per service rewrites them once
  in sorted order, and it is stable from then on.

## Tests

`pkg/controller/sync_device_types_test.go` covers both invariants without any infrastructure:
the first by asserting the caller's device type is unchanged after an early return, the second
by preparing the same event 50 times and against itself as the base. Both were confirmed to
fail against the unfixed code.

`go test ./...` is safe to run: the package's other test file is local and gitignored, and its
one function — which talks to a live ChirpStack instance and renames tenants — is disabled by
a name that `go test` does not pick up. Check that before re-enabling it.
