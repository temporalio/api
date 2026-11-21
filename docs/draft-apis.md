# Draft API annotations

This specification defines how `api` generates the stable proto tree, how `api-go` generates draft filtering helpers for `api_next`, and how the Temporal server gates, filters, and analyzes draft APIs. The [implementation status](#implementation-status) records what the current branches support.

## Placements and effects

The options live in [`temporal/api_next/protometa/v1/draft.proto`](../temporal/api_next/protometa/v1/draft.proto) in the `temporal.api.protometa.v1` package. Import that proto directly. Each option is a singular string containing one nonempty, whitespace-free, case-sensitive feature name. For example, `priority` and `Priority` are distinct names. `""` and `"high priority"` are invalid.

| Declaration | Option and placement | Stable tree | `api_next` Go helpers and server runtime when disabled | Server analyzer |
| --- | --- | --- | --- | --- |
| Field, including a new member of a stable oneof | `draft` (`FieldOptions`) | Remove the field. | Clear the field or unset the active oneof member. | Require acknowledgment for the field, its getter, and its oneof wrapper where applicable. |
| Message | `draft_message` (`MessageOptions`) | Remove the message and its contents. | Clear fields that use the type. RPC endpoint types must match their method or service annotation. | Require acknowledgment for the type, its draft contents, and references that use them. |
| New enum | `draft_enum` (`EnumOptions`) | Remove the enum and its values. | Clear fields that use the enum type. | Require acknowledgment for the enum and its values. |
| New value of a stable enum | `draft_enum_value` (`EnumValueOptions`) | Remove the value and retain the enum. | Reject surviving input occurrences with `codes.InvalidArgument`. On output, clear singular fields, drop repeated elements, and delete map entries containing the value. | Require acknowledgment for the value. |
| New oneof | `draft_oneof` (`OneofOptions`) | Remove the oneof and its members. | Unset the oneof. | Require acknowledgment for the oneof and its member getters and wrappers. |
| New service | `draft_service` (`ServiceOptions`) | Remove the service and its RPCs. | For a covered service, gate every RPC. A disabled RPC returns `codes.Unimplemented`. | Require acknowledgment for generated service and method symbols. |
| RPC in a stable service | `draft_method` (`MethodOptions`) | Remove the RPC and retain the service. | For a covered service, a disabled RPC returns `codes.Unimplemented`. | Require acknowledgment for generated symbols that name the draft RPC. |

These seven annotations are the complete set. The removed options are `draft_file` and `draft_extension`. Protobuf extension declarations have no draft annotation placement.

## Declaration rules

### Containers and nesting

Before projection, check source declarations, excluding synthetic oneofs for proto3 `optional` fields. A nonempty enum, oneof, or service that has no draft annotation and does not inherit draft status from an enclosing declaration is a schema error if all its members are draft. It must carry `draft_enum`, `draft_oneof`, or `draft_service`, respectively. Count both explicit annotations and implicit draft status from type references. For example, a oneof whose branches all use draft message types requires `draft_oneof` even when the fields carry no annotations. A service declared with zero RPCs is stable unless it carries `draft_service`.

Members of draft containers, including enums and oneofs nested in a `draft_message`, inherit the container's feature and must carry no draft annotation of their own, even for the same feature.

Messages are exempt from the all-members rule. A stable message, including a previously empty message, may gain only draft fields and remain stable after those fields are removed.

```proto
// Valid: keep the stable enum and oneof, and remove their draft members.
enum Priority {
  PRIORITY_UNSPECIFIED = 0;
  PRIORITY_HIGH = 1 [(temporal.api.protometa.v1.draft_enum_value) = "priority"];
}
message Selector {
  oneof key {
    string id = 1;
    string alias = 2 [(temporal.api.protometa.v1.draft) = "aliases"];
  }
}

// Valid: annotate the new containers and leave their members unannotated.
enum PreviewMode {
  option (temporal.api.protometa.v1.draft_enum) = "preview";
  PREVIEW_MODE_UNSPECIFIED = 0;
}
message Routing {
  oneof destination {
    option (temporal.api.protometa.v1.draft_oneof) = "routing";
    PreviewConfig.Nested target = 1;
  }
}

// Valid: stable projection leaves OptionalSettings empty.
message OptionalSettings {
  bool precise = 1 [(temporal.api.protometa.v1.draft) = "precision"];
}
```

`Routing.destination.target` requires both `routing` from its oneof and `preview` from its type (`PreviewConfig.Nested`, defined below). Adding `draft_enum_value` to `PREVIEW_MODE_UNSPECIFIED` or `draft` to `target` is a nesting error. Removing `draft_oneof` from `Routing.destination` is a container error because `target` uses a draft type. Marking both branches of `Selector.key` with `draft` is also a container error.

In a stable enum, a draft value cannot have number zero. A draft value must have a unique number within its enum. Aliasing involving a draft value is a schema error, even with `allow_alias = true`, so each wire number in an enum maps to at most one draft feature. For example, adding `PRIORITY_PREVIEW = 1` as an alias of `PRIORITY_HIGH` is invalid whether `PRIORITY_PREVIEW` is stable or draft.

### RPC signatures and services

In a stable service, an RPC with a draft request or response must carry `draft_method`. Both endpoint types must explicitly carry `draft_message` with the method's feature. An unannotated RPC using draft types, an annotated RPC with a stable endpoint, or a mismatch between feature names is a schema error. Inherited draft status alone cannot satisfy the endpoint requirement. No RPC becomes draft implicitly through its types.

In a service with `draft_service`, every RPC is covered by the service annotation and must omit `draft_method`. Both endpoint types of every RPC must explicitly carry `draft_message` with the service's feature.

A message may occupy only one request or response position across the entire `api_next` tree. Using the same message in both positions of one RPC is always invalid. The generator checks all services and files without a historical baseline. Grandfathered stable sharing is limited to the exact positions in a checked-in allowlist that identifies each fully qualified RPC and its request or response position. Any additional use of a grandfathered type is an error. The allowlist starts empty.

```proto
message PreviewRequest {
  option (temporal.api.protometa.v1.draft_message) = "preview";
}
message PreviewResponse {
  option (temporal.api.protometa.v1.draft_message) = "preview";
}
service PreviewService {
  option (temporal.api.protometa.v1.draft_service) = "preview";
  rpc Preview(PreviewRequest) returns (PreviewResponse);
}
```

Alternatively, `Preview` can be added to an existing service alongside its stable RPCs:

```proto
rpc Preview(PreviewRequest) returns (PreviewResponse) {
  option (temporal.api.protometa.v1.draft_method) = "preview";
}
```

Removing the annotation from `PreviewResponse`, changing its feature, reusing `PreviewRequest` in another RPC, or writing `rpc Preview(PreviewRequest) returns (PreviewRequest)` is invalid. A service containing only methods marked `draft_method` requires `draft_service` and must remove the method annotations.

### Propagation and feature combination

A declaration needs its own feature, every feature inherited from its enclosing draft declarations, and every feature required by the draft types it uses. All required features must be enabled. A field inherits the requirements of its message or enum type, including the value type of a map and the element type of a repeated field. An unannotated use inherits the same requirements. A stable message stays stable when it contains draft fields. Its callers retain their message fields and filter the draft contents recursively. A draft value in a stable enum likewise does not make every field of that enum type draft.

```proto
message PreviewConfig {
  option (temporal.api.protometa.v1.draft_message) = "preview";
  // Nested inherits "preview" without an annotation.
  message Nested {}
}
message Config {
  PreviewConfig.Nested defaults = 1;
  map<string, PreviewConfig> profiles = 2;
  PreviewMode mode = 3;
  PreviewConfig override = 4 [(temporal.api.protometa.v1.draft) = "overrides"];
}
```

`defaults`, `profiles`, and `mode` are implicitly draft. `override` requires both `preview` and `overrides`. All four fields are removed from the stable tree.

### Field removal and numbers

Draft field numbers may change before the field first lands in `api_next`. When removing a landed draft field from `api_next`, reserve both its number and its name in the containing message. `buf breaking` with [`buf.next.yaml`](../buf.next.yaml) enforces both reservations against the previous preview schema. For example, after deleting `precise` from `OptionalSettings`:

```proto
message OptionalSettings {
  reserved 1;
  reserved "precise";
}
```

## Stable tree generation

`cmd/generate-stable-protos` validates the source schema before projection, including the all-draft container rule. For valid input, it removes explicitly and implicitly draft declarations and their comments, including whole draft containers. Retain stable containers, including services declared with zero RPCs.

For retained RPCs, reject reference paths in `google.api.http` and `request_header` that name removed fields, including nested paths. For example, if `LookupRequest.alias` is a draft field, `option (google.api.http) = { get: "/v1/{alias}" };` is invalid on a retained RPC. Removed draft RPCs skip these stable-projection reference checks and remain subject to normal protobuf compilation and draft signature validation.

Reject any protobuf extension declaration whose value type, extendee, or declaring parent is a draft type.

## Runtime filtering

`api-go` generates `OmitDraftFields(enabled func(string) bool)` for affected `api_next` Go types. It clears draft fields and fields that use draft types whenever any required feature is disabled. Its scope is field omission. It never checks or filters individual enum values.

Generate both enum helpers only for messages that can reach an enum field whose type declares at least one individually annotated `draft_enum_value`. Determine reachability statically through singular fields, oneof members, repeated elements, and map values.

- `CheckDraftEnumValues(enabled func(string) bool) error` returns an error naming the field path and value if any singular value, repeated element, or map value is a disabled draft enum value.
- `OmitDraftEnumValues(enabled func(string) bool)` clears singular fields, drops repeated elements, and removes map entries that hold disabled draft enum values. Clearing a singular field removes explicit presence or unsets its active oneof branch where applicable.

On requests, run `OmitDraftFields` before `CheckDraftEnumValues` so disabled fields are cleared before their values are checked. The server returns `codes.InvalidArgument` if the check fails. On responses and outbound protobuf error details, run `OmitDraftFields` before `OmitDraftEnumValues`. For example, if `priority` is disabled, a request with `PRIORITY_HIGH` anywhere in `repeated Priority priorities` is rejected. The same element is dropped from a response, preserving the other elements.

The server reads draft RPC features from method and service descriptors. On public Temporal API services registered by the frontend, a draft RPC returns `codes.Unimplemented` before dispatch if its feature is disabled. This scope includes `WorkflowService`, `OperatorService`, and future public Temporal API services registered by the frontend.

The RPC gate and payload filters share one feature selection per invocation. Use the namespace-filtered dynamic config value for the supplied namespace name. If the request supplies only a namespace ID, resolve it to a name and use that namespace's value. If the lookup fails, disable all draft features. Requests without a namespace name or ID use the global default feature set, the dynamic config value without a namespace filter.

Payload filtering covers requests, responses, and protobuf error details on unary calls to those public Temporal API services, and in the Nexus handlers. Enum helper dispatch is independent of the presence of `OmitDraftFields` and of any fast path that checks whether field omission is needed. Apply enum handling to roots that only need enum handling, including requests, responses, and error details.

Both the RPC gate and payload filtering exclude `AdminService`, internal services such as `HistoryService` and `MatchingService`, and infrastructure services such as gRPC health and reflection. A guard test enumerates the frontend's actual service registrations and requires each service to have both kinds of coverage or appear on an explicit infrastructure/internal exclusion list, which includes `AdminService`, health, and reflection. A separate guard test fails if a covered service adds a streaming RPC. Payload filtering covers unary RPCs and leaves streaming payloads out of scope.

The server unwraps recognized protobuf status details from their `google.protobuf.Any` envelopes and applies outbound filtering to each decoded detail as a root. Unrecognized envelopes pass through, and `Any` fields inside decoded details or other message roots remain opaque.

Payload filtering preserves unknown fields on retained messages. When a disabled draft message is passed directly to `OmitDraftFields` as the root, `proto.Reset` clears the whole message, including its unknown fields. This whole-root reset is an exception to unknown-field preservation.

## Server analyzer

`draftapi` requires a `temporal:allow-draft-api` acknowledgment for named Go references to explicitly or implicitly draft declarations. This includes enum types and values, oneof wrappers, and generated service and method symbols for draft RPCs. An acknowledgment must cover every required feature. It does not enable features at runtime.

```go
_ = cfg.GetOverride() // temporal:allow-draft-api preview overrides
```

## Implementation status

| Repository | Implemented today | Required by this specification |
| --- | --- | --- |
| `api` | `draft.proto` declares all seven options. Before projection, the generator validates direct imports, annotation placement, feature names, containers and nesting, enum numbers and aliases, RPC signatures and endpoint uniqueness, and extension references to draft types. It checks HTTP and request-header field references on retained RPCs. Stable projection removes draft messages and fields, including fields that reference draft types, along with their comments. Buf checks preview reservations. | Complete stable projection for the new placements and extend the independent descriptor oracle to check their removal and surviving comments. |
| `api-go` | `protoc-gen-go-helpers` accepts field and message annotations and generates recursive omission through affected messages, including message-valued maps. It combines field and type features, including draft status inherited by enums nested in draft messages, and clears draft members of stable oneofs. It resets a disabled draft root with `proto.Reset`, clearing unknown fields. It rejects RPC signatures and extensions using draft types and checks HTTP and request-header references to removed fields. | Accept all seven placements and valid draft RPC signatures. Remove recognition of `draft_file` and `draft_extension`. Limit RPC option reference checks to retained RPCs and keep the extension checks. Extend field omission to explicitly draft enum types and whole draft oneofs. Generate both enum value helpers based on reachability to `draft_enum_value`. |
| Temporal server | `draftapi` accepts field and message placements and tracks their generated Go references, including affected oneof wrappers and enums and values inherited from draft messages. The runtime omits fields in covered unary requests, responses, error details, and Nexus paths. The unary-only guard test exists. | Remove recognition of `draft_file` and `draft_extension`. Extend the analyzer to the new placements and generated RPC and service symbols. Add descriptor-based RPC gating in covered public Temporal services and the frontend registration guard. Dispatch enum helpers independently of field omission, including for enum-only request, response, and error-detail roots. |

### PR order and verification

1. **`api`** supplies the annotation schema, stable projection, and shared classification fixtures. Extend the independent descriptor oracle to compare the projected schema and surviving comments for the new placements. Add shared fixtures for valid and invalid declarations, including all-draft containers, endpoint reuse, enum aliases, and retained versus removed RPC option references.
2. **`api-go`** takes the updated `api` revision, consumes the shared classification fixtures, and regenerates the Go trees and helper fixtures. Extend `draftwire` to check that outbound field and enum omission with features disabled matches the stable projection, and that enabling all features preserves the input. Build the expected result independently of the generated helpers. Walk a copy of the original payload using the `api_next` descriptors and remove only enum occurrences whose numbers have a `draft_enum_value` annotation with a disabled feature. Clear singular fields, drop matching repeated elements and map entries, and preserve all other values, including unrecognized enum numbers. Marshal that copy, decode it against stable descriptors with `DiscardUnknown`, and compare it with the filtered output decoded against the same descriptors. `DiscardUnknown` alone preserves unrecognized proto3 enum numbers in known fields, including numbers of projected-out enum values.
3. **Temporal server** takes the regenerated `api-go/api_next` revision. Extend the `draftapi` contract test to cover the new generated symbols and their required features. Extend the descriptor-based filter properties to cover RPC gating and enum-only roots, including request rejection and omission from responses and error details. Check that a disabled field is cleared before enum validation and that the frontend registration guard accounts for every registered service.
