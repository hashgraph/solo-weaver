# Adding a New Label Profile

Label profiles control which labels are injected into Alloy metrics and logs.
Each profile is a self-contained Go file implementing the `labels.Profiler` interface.

The `cluster` label is managed by all profiles — every profile must include it.
The default profile is `eng`, which includes only the `cluster` label.

## Default Profile

The `eng` profile is used by default when no `labelProfile` is specified on a remote.
It produces only the `cluster` label (derived from `--cluster-name`).

## Architecture

| File / Package            | Role                                                              |
|---------------------------|-------------------------------------------------------------------|
| `internal/alloy/labels/profile.go` | `LabelInput` (runtime data bag), `Profiler` interface, shared registry, validation (`IsValid`, `ValidNames`), resolution (`Resolve`), `DefaultProfile` constant |
| `internal/alloy/labels/render.go`  | Rendering (`RenderLabelRules`, `RenderStaticLabels`)              |
| `internal/alloy/labels/eng.go`     | Eng profile (default) — cluster label only                        |
| `internal/alloy/labels/ops.go`     | Ops profile implementation (template to copy) and shared helpers: `ParseClusterName`, `extractAlphaPrefix` |

## Steps

### 1. Create the profile file

Copy `ops.go` to a new file, e.g. `sre.go`, inside `internal/alloy/labels/`:

```go
package labels

// SreProfile adds labels used by the SRE team.
type SreProfile struct{}

func init() {
    Register(SreProfile{})
}

func (SreProfile) Name() string { return "sre" }

// Labels returns the complete label set for the sre profile.
//
// Labels added (from LabelInput):
//   - cluster        = ClusterName (mandatory)
//   - environment    = DeployProfile
//   - instance_type  = alphabetic prefix of first cluster name segment (e.g. "lfh")
//   - team           = "sre"
func (SreProfile) Labels(input LabelInput) map[string]string {
    labels := ParseClusterName(input.ClusterName)
    if input.ClusterName != "" {
        labels["cluster"] = input.ClusterName
    }
    if input.DeployProfile != "" {
        labels["environment"] = input.DeployProfile
    }
    labels["team"] = "sre"
    return labels
}
```

Every profile **must** include the `cluster` label (derived from `ClusterName`).

`ParseClusterName()` (in `ops.go`) extracts common base labels
(`instance_type`) from the cluster name.
Add `cluster` from `input.ClusterName`, `environment` from `input.DeployProfile`,
and any profile-specific labels on top.
If your profile needs completely custom labels, skip `ParseClusterName()`
but still include `cluster`.

Registration via `Register()` in `init()` is all that's needed —
`pkg/models` validates against the shared registry automatically.

### 2. Update CLI help text

In `cmd/weaver/commands/alloy/cluster/cluster.go`, update the
`--add-prometheus-remote` and `--add-loki-remote` flag descriptions:

```
labelProfile=eng|ops|sre
```

### 3. Add tests

Create `sre_test.go` (or add to `ops_test.go`) verifying
`SreProfile{}.Labels(LabelInput{ClusterName: "...", DeployProfile: "..."})` returns the expected label map.
Add validation test cases in `pkg/models/validation_test.go`
confirming the new profile is accepted.
