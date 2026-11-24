# Draft API annotations

This specification defines how `api` generates the stable proto tree, how `api-go` generates draft filtering helpers for `api_next`, and how the Temporal server gates, filters, and analyzes draft APIs. The [implementation status](#implementation-status) records what the current branches support.

## Placements and effects

The options live in [`temporal/api_next/protometa/v1/draft.proto`](../temporal/api_next/protometa/v1/draft.proto) in the `temporal.api.protometa.v1` package. Import that proto directly. Each option is a singular string containing one nonempty, whitespace-free, case-sensitive feature name. For example, `priority` and `Priority` are distinct names. `""` and `"high priority"` are invalid.

| Declaration | Option and placement | Stable tree | `api_next` Go helpers and server runtime when disabled | Server analyzer |
| --- | --- | --- | --- | --- |
| Field, including a new member of a stable oneof | `draft` (`FieldOptions`) | Remove the field. | Clear the field or unset the active oneof member. | Require acknowledgment for the field, its getter, and its oneof wrapper where applicable. |
| Message | `draft_message` (`MessageOptions`) | Remove the message and its contents. | Clear fields that use the type. RPC endpoint types must match their method or service annotation. | Require acknowledgment for the type, its draft contents, and references that use them. |
| New enum | `draft_enum` (`EnumOptions`) | Remove the enum and its values. | Clear fields that use the enum type. | Require acknowledgment for the enum and its values. |
| New value of a stable enum | `draft_enum_value` (`EnumValueOptions`) | Remove the value and retain the enum. | Clear singular fields, drop repeated elements, and delete map entries containing the value. Reject a request with `codes.InvalidArgument` if this removes a value from a retained field. | Require acknowledgment for the value. |
| New oneof | `draft_oneof` (`OneofOptions`) | Remove the oneof and its members. | Unset the oneof. | Require acknowledgment for the oneof and its member getters and wrappers. |
| New service | `draft_service` (`ServiceOptions`) | Remove the service and its RPCs. | For a covered service, gate every RPC. A disabled RPC returns `codes.Unimplemented`. | Require acknowledgment for generated service and method symbols. |
| RPC in a stable service | `draft_method` (`MethodOptions`) | Remove the RPC and retain the service. | For a covered service, a disabled RPC returns `codes.Unimplemented`. | Require acknowledgment for generated symbols that name the draft RPC. |

These seven annotations are the complete set. The removed options are `draft_file` and `draft_extension`. Protobuf extension declarations have no draft annotation placement.

## Declaration rules

Every proto file in `api_next` must declare `syntax = "proto3";`. This rule applies to the source tree. External imports may use other syntax versions or editions.

### Containers and nesting

Before projection, check source declarations, excluding synthetic oneofs for proto3 `optional` fields. A nonempty enum, oneof, or service that has no draft annotation and does not inherit draft status from an enclosing declaration is a schema error if all its members are annotated as draft. It must carry `draft_enum`, `draft_oneof`, or `draft_service`, respectively. A service declared with zero RPCs is stable unless it carries `draft_service`.

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
    option (temporal.api.protometa.v1.draft_oneof) = "preview";
    PreviewConfig.Nested target = 1;
  }
}

// Valid: stable projection leaves OptionalSettings empty.
message OptionalSettings {
  bool precise = 1 [(temporal.api.protometa.v1.draft) = "precision"];
}
```

`Routing.destination.target` inherits `preview` from its oneof, matching the feature of its type (`PreviewConfig.Nested`, defined below). Adding `draft_enum_value` to `PREVIEW_MODE_UNSPECIFIED` or `draft` to `target` is a nesting error. Removing `draft_oneof` from `Routing.destination` leaves `target` without the required annotation. Marking both branches of `Selector.key` with `draft` is a container error.

In a stable enum, a draft value cannot have number zero. A draft value must have a unique number within its enum. Aliasing involving a draft value is a schema error, even with `allow_alias = true`, so each wire number in an enum maps to at most one draft feature. For example, adding `PRIORITY_PREVIEW = 1` as an alias of `PRIORITY_HIGH` is invalid whether `PRIORITY_PREVIEW` is stable or draft.

### RPC signatures and services

In a stable service, an RPC with a draft request or response must carry `draft_method`. Both endpoint types must explicitly carry `draft_message` with the method's feature. An unannotated RPC using draft types, an annotated RPC with a stable endpoint, or a mismatch between feature names is a schema error. Inherited draft status alone cannot satisfy the endpoint requirement. No RPC becomes draft implicitly through its types.

In a service with `draft_service`, every RPC is covered by the service annotation and must omit `draft_method`. Both endpoint types of every RPC must explicitly carry `draft_message` with the service's feature.

A draft endpoint type may occupy only one request or response position across the entire `api_next` tree, so using it in both positions of one RPC is invalid. The generator checks all services and files without a historical baseline. Stable RPCs may share endpoint types, such as `google.protobuf.Empty`, and may use one type in both positions; they cannot use draft types at all.

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

### Containment and type references

Every draft declaration has exactly one feature. Containment is the only source of implicit draft status. A field in a stable parent that uses a draft message or enum must carry `draft` with the type's feature. This includes the element type of a repeated field, the value type of a map, and nested types that inherit a feature. A field inside a draft container inherits that container's feature and can reference draft types only if they have the same feature. Missing annotations and feature mismatches are validation errors that identify the field and the referenced draft type.

A message with draft fields and an enum with draft values can each remain stable. Fields that use those stable types can remain stable, and runtime filtering visits the draft contents recursively.

A retained list or map must be safe to send without filtering individual entries. A stable collection cannot reach any draft fields, types, or enum values through its element or value type. A draft collection can reach declarations with the same feature as the collection, because retaining it means that feature is enabled. A field inside a draft message also inherits that message's feature for this check.

```proto
message PreviewConfig {
  option (temporal.api.protometa.v1.draft_message) = "preview";
  // Nested inherits "preview" without an annotation.
  message Nested {}
}
message Config {
  PreviewConfig.Nested defaults = 1 [(temporal.api.protometa.v1.draft) = "preview"];
  map<string, PreviewConfig> profiles = 2 [(temporal.api.protometa.v1.draft) = "preview"];
  PreviewMode mode = 3 [(temporal.api.protometa.v1.draft) = "preview"];
  PreviewConfig override = 4 [(temporal.api.protometa.v1.draft) = "preview"];
}
```

All four fields are explicitly draft and are removed from the stable tree. Changing the feature of `override` to `overrides` is a validation error because `PreviewConfig` has feature `preview`.

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

Place comments meant to disappear inside the draft declaration, directly above it without a blank line, or on its last line. The generator rejects next-line trailing comments and detached comments that would survive removal. A comment between a removed declaration and the next retained declaration can stay when it is part of the doc block directly above the retained declaration. Move a comment away from the gap before a closing brace or end of file to place it with the declaration it describes.

For retained RPCs, reject reference paths in `google.api.http` and `request_header` that name removed fields, including nested paths. For example, if `LookupRequest.alias` is a draft field, `option (google.api.http) = { get: "/v1/{alias}" };` is invalid on a retained RPC. Removed draft RPCs skip these stable-projection reference checks and remain subject to normal protobuf compilation and draft signature validation.

Option literals on retained declarations must reference only retained declarations, including fields and enum values in aggregates and typed `Any` payloads. The generator recompiles the projection before writing output and reports the compiler diagnostic for dangling references. Open proto3 enum numbers can still compile as unknown values, so recompilation cannot identify every numeric encoding of a removed enum value. Options on removed declarations may reference draft declarations.

Reject any protobuf extension declaration whose value type, extendee, or declaring parent is a draft type.

## Runtime filtering

`api-go` generates a single method, `OmitDraftFields(enabled func(string) bool) (removedEnumValue bool)`, for affected `api_next` Go types. Determine reachability statically through singular fields and oneof members. A repeated or map field contributes its own feature gate. Generate the method for roots that reach draft fields or containers and for roots that reach only individually annotated draft enum values.

In one traversal, the method clears fields and oneofs whose features are disabled and skips their contents. The helper clears draft lists and maps as whole fields and leaves the entries of retained collections in place. It clears disabled draft enum values from retained singular fields. Clearing a singular field also removes explicit presence or unsets its active oneof member where applicable. The return value is true if the traversal removed at least one disabled draft enum value from a retained field, including in a nested message. Removing an entire disabled field or container leaves the return value false unless the traversal finds a disabled enum value elsewhere.

On requests, the server rejects a true result with `codes.InvalidArgument` and a generic message such as “Request contains a disabled draft enum value.” The message never names the enum, value, or field. On responses and outbound protobuf error details, the server applies the same method and ignores its result. For example, with `priority` disabled, a request with `PRIORITY_HIGH` in a retained `Priority priority` field is rejected. The filter clears that field on a response.

The server reads draft RPC features from method and service descriptors. On public Temporal API services registered by the frontend, a draft RPC returns `codes.Unimplemented` before dispatch if its feature is disabled. This scope includes `WorkflowService`, `OperatorService`, and future public Temporal API services registered by the frontend.

The RPC gate and payload filters share one feature selection per invocation. Use the namespace-filtered dynamic config value for the supplied namespace name. If the request supplies only a namespace ID, resolve it to a name and use that namespace's value. If the lookup fails, disable all draft features. Requests without a namespace name or ID use the global default feature set, the dynamic config value without a namespace filter.

Payload filtering covers requests, responses, and protobuf error details on unary calls to those public Temporal API services, and in the Nexus handlers. Invoke `OmitDraftFields` on affected roots, including those whose only draft contents are enum values.

Both the RPC gate and payload filtering exclude `AdminService`, internal services such as `HistoryService` and `MatchingService`, and infrastructure services such as gRPC health and reflection. A guard test enumerates the frontend's actual service registrations and requires each service to have both kinds of coverage or appear on an explicit infrastructure/internal exclusion list, which includes `AdminService`, health, and reflection. A separate guard test fails if a covered service adds a streaming RPC. Payload filtering covers unary RPCs and leaves streaming payloads out of scope.

The server unwraps recognized protobuf status details from their `google.protobuf.Any` envelopes and applies outbound filtering to each decoded detail as a root. Unrecognized envelopes pass through, and `Any` fields inside decoded details or other message roots remain opaque.

Payload filtering preserves unknown fields on retained messages. When a disabled draft message is passed directly to `OmitDraftFields` as the root, `proto.Reset` clears the whole message, including its unknown fields. This whole-root reset is an exception to unknown-field preservation.

## Server analyzer

`draftapi` requires a `temporal:allow-draft-api` acknowledgment for named Go references to declarations that are explicitly draft or inherit draft status from a container. This includes enum types and values, oneof wrappers, and generated service and method symbols for draft RPCs. The acknowledgment covers the feature of each referenced draft declaration. It does not enable features at runtime.

```go
_ = cfg.GetOverride() // temporal:allow-draft-api preview
```

## Implementation status

| Repository | Implemented today | Required by this specification |
| --- | --- | --- |
| `api` | `draft.proto` declares all seven options. Before projection, the generator validates proto3 syntax, direct imports, annotation placement, feature names, containers and nesting, matching features on field type references, enum numbers and aliases, enum values in retained options, RPC signatures and draft endpoint uniqueness, and extension references to draft types. It checks HTTP and request-header field references on retained RPCs. Stable projection removes all seven draft placements, their contained declarations, unused imports, and comments owned by removed declarations and imports. The independent descriptor oracle checks the projected schema, surviving comments, and attachment by descriptor path. Buf checks preview reservations. | Implemented. |
| `api-go` | `protoc-gen-go-helpers` accepts field and message annotations and generates recursive omission through affected messages, including message-valued maps. It infers draft status through type references and clears draft members of stable oneofs. It resets a disabled draft root with `proto.Reset`, clearing unknown fields. It rejects RPC signatures and extensions using draft types and checks HTTP and request-header references to removed fields. | Accept all seven placements, enforce one feature per declaration with implicit draft status only through containment, and accept valid draft RPC signatures. Remove recognition of `draft_file` and `draft_extension`. Limit RPC option reference checks to retained RPCs and keep the extension checks. Extend `OmitDraftFields` to cover whole draft oneofs and reachable draft enum values, and return whether it removed a disabled enum value from a retained field. |
| Temporal server | `draftapi` accepts field and message placements and tracks their generated Go references, including affected oneof wrappers and enums and values inherited from draft messages. The runtime omits fields in covered unary requests, responses, error details, and Nexus paths. The unary-only guard test exists. | Remove recognition of `draft_file` and `draft_extension`. Extend the analyzer to the new placements and generated RPC and service symbols, using a single feature per declaration. Add descriptor-based RPC gating in covered public Temporal services and the frontend registration guard. Invoke the unified `OmitDraftFields` on affected roots and use its result to reject requests with a generic `codes.InvalidArgument` error. |

### PR order and verification

1. **`api`** supplies the annotation schema, stable projection, and shared classification fixtures for valid and invalid declarations, including field type references, all-draft containers, endpoint reuse, enum aliases, and retained versus removed RPC option references. The independent descriptor oracle compares the projected schema and surviving comments for all placements.
2. **`api-go`** takes the updated `api` revision, consumes the shared classification fixtures, and regenerates the Go trees and helper fixtures. Extend `draftwire` to check that field and enum omission with features disabled matches the stable projection and that enabling all features preserves the input. Build the expected result independently of the generated helper. Walk a copy of the original payload using the `api_next` descriptors. Clear disabled fields and containers and skip their contents. From retained singular fields, remove enum occurrences whose numbers have a `draft_enum_value` annotation with a disabled feature. Preserve all other values, including unrecognized enum numbers, and leave retained collection entries in place. Check that the helper's result reports whether the walk removed an enum value from a retained field. Marshal the copy, decode it against stable descriptors with `DiscardUnknown`, and compare it with the filtered output decoded against the same descriptors. `DiscardUnknown` alone preserves unrecognized proto3 enum numbers in known fields, including numbers of projected-out enum values.
3. **Temporal server** takes the regenerated `api-go/api_next` revision. Extend the `draftapi` contract test to cover the new generated symbols and their features. Extend the descriptor-based filter properties to cover RPC gating and enum-only roots, including generic request rejection and omission from responses and error details. Check that enum values inside disabled fields do not trigger rejection and that the frontend registration guard accounts for every registered service.
